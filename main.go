package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	"github.com/dgryski/go-metro"
)

type File struct {
	path      string
	size      uint64
	bytesHash uint64
	fullHash  uint64
}

func FileFromPath(path string, buffSize int) (File, error) {
	firstBytes := make([]byte, buffSize)
	file, err := os.Open(path)
	defer file.Close()

	stats, _ := file.Stat()
	if err != nil {
		return File{}, err
	}

	_, err = file.ReadAt(firstBytes, 0)
	if err != nil {
		if err != io.EOF {
			return File{}, err
		}
	}
	return File{path: path, size: uint64(stats.Size()), bytesHash: metro.Hash64(firstBytes, 0)}, nil
}

type PathStore struct {
	sync.Mutex
	paths        map[uint64][]File
	bytesPerFile int
	pathsAdded map[uint64]bool
}

func NewPathStore(paths []string, bytesPerFile int) (*PathStore, error) {
	store := &PathStore{paths: make(map[uint64][]File), pathsAdded: make(map[uint64]bool), bytesPerFile: bytesPerFile}
	for _, p := range paths {
		err := store.AddFile(p)
		if err != nil {
			return store, err
		}
	}
	return store, nil
}

func (p *PathStore) PathAdded(path string) bool {
	_, ok := p.pathsAdded[metro.Hash64([]byte(path), 0)]
	if ok {
		return true
	}
	return false
}

func (p *PathStore) AllPaths() []File {
	var ps []File
	p.Lock()
	for _, v := range p.paths {
		for _, x := range v {
			ps = append(ps, x)
		}
	}
	p.Unlock()
	return ps
}

func (p *PathStore) EmptyFiles() []File {
	var emptyFiles []File
	p.Lock()
	for _, v := range p.paths {
		for _, f := range v {
			if f.size == 0 {
				emptyFiles = append(emptyFiles, f)
			}
		}
	}
	p.Unlock()
	return emptyFiles
}

func (p *PathStore) AddFile(s string) error {
	if p.PathAdded(s) {
		return errors.New("Path already present")
	}
	f, err := FileFromPath(s, p.bytesPerFile)
	if err != nil {
		return err
	}
	p.Lock()
	p.paths[f.bytesHash] = append(p.paths[f.bytesHash], f)
	p.Unlock()
	return nil
}

func (p *PathStore) FileCount() int64 {
	count := 0
	p.Lock()
	for _, v := range p.paths {
		count += len(v)
	}
	p.Unlock()
	return int64(count)
}

func (p *PathStore) FileSetCount() int64 {
	p.Lock()
	length := len(p.paths)
	p.Unlock()
	return int64(length)
}

func (p *PathStore) Prune() *PathStore {
	newStore := &PathStore{paths: make(map[uint64][]File), pathsAdded: make(map[uint64]bool), bytesPerFile: p.bytesPerFile}
	p.Lock()
	for k, v := range p.paths {
		if len(v) > 1 {
			newStore.paths[k] = v
		}
	}
	p.Unlock()
	return newStore
}

func (p *PathStore) TotalSizeDups() int64 {
	total := 0
	p.Lock()
	for _, v := range p.paths {
		// We only want the size of the duplicated files, so the size of the duplicates
		// is the size of the files (since they all have the same size) multiplied
		// by the number of times the file is duplicated (1 less than the total number of files in the set)
		total += (len(v) - 1) * int(v[0].size)
	}
	p.Unlock()
	return int64(total)
}

func (p *PathStore) Summarize() string {
	numFiles := strconv.FormatInt(int64(p.FileCount()), 10)
	numSets := strconv.FormatInt(int64(p.FileSetCount()), 10)
	sizeMegabytes := strconv.FormatInt(p.TotalSizeDups()/(1024*1024), 10)
	return numFiles + " files (in " + numSets + " sets), occupying " + sizeMegabytes + " megabytes"
}

//TODO(Adam Drake): have this take a channel of paths and a return channel of files?
func fromSTDIn() *PathStore {
	scr := bufio.NewScanner(bufio.NewReader(os.Stdin))
	themap := PathStore{paths: make(map[uint64][]File)}
	for scr.Scan() {
		err := themap.AddFile(scr.Text())

		if err != nil {
			errOut(err)
		}
	}
	return themap.Prune()
}

func fileCheck(paths *[]string, path string, info os.FileInfo, err error) error {
	if err != nil || info.IsDir() || (info.Mode()&os.ModeSymlink == os.ModeSymlink) {
		return nil
	}
	*paths = append(*paths, path)
	return nil
}

func dirWalk(start string) ([]string, error) {
	var paths []string
	fileFunc := func(path string, fi os.FileInfo, err error) error {
		return fileCheck(&paths, path, fi, err)
	}
	err := filepath.Walk(start, fileFunc)
	if err != nil {
		return paths, err
	}
	return paths, nil

}

func hashWorker(inq chan File, res chan File, wg *sync.WaitGroup) {
	defer wg.Done()
	for f := range inq {
		data, err := ioutil.ReadFile(f.path) //TODO(Adam Drake): convert this to a streaming hash to save memory
		if err != nil {
			errOut(err)
		}
		f.fullHash = metro.Hash64(data, 0)
		res <- f
	}
}

func errOut(e error) {
	fmt.Println(e)
	os.Exit(1)
}

func main() {
	stdin := flag.Bool("stdin", false, "Read pathes from STDIN?")
	path := flag.String("path", "", "Starting path")
	//recurse := flag.Bool("r", false, "Walk the directory tree recursively?") //TODO(Adam Drake): enable toggle and make sure to accept N for levels of recursion
	numWorkers := flag.Int("workers", 2*runtime.NumCPU(), "Number of workers for hashing")
	numBytes := flag.Int("bytes", 4096, "Compare the first X bytes of each file")
	summarize := flag.Bool("summarize", false, "Output only summary statistics")
	flag.Parse()

	if *stdin && (*path != "") {
		fmt.Println("Only one of -path or -stdin may be used")
		os.Exit(1)
	}


	var bytesMatch *PathStore
	if *stdin {
		fmt.Println("getting paths from stdin")
		bytesMatch = fromSTDIn()
	} else {

		paths, err := dirWalk(*path)
		if err != nil {
			errOut(err)
		}
		bytesMatch, err = NewPathStore(paths, *numBytes)
		if err != nil {
			errOut(err)
		}
	}

	fmt.Println(bytesMatch.Summarize())

	//current key is hash of first X bytes of file
	newStore := bytesMatch.Prune()
	fmt.Println(newStore.Summarize())

	//do hashing of each file
	var wg sync.WaitGroup
	hashq := make(chan File)
	resultq := make(chan File)
	go func() {
		wg.Wait()
		close(resultq)
	}()
	hashed := &PathStore{paths: make(map[uint64][]File)}

	fmt.Println("got the hashed map")

	go func() {
		for _, v := range newStore.paths {
			for _, f := range v {
				hashq <- f
			}
		}
		close(hashq)
	}()

	for i := 0; i < *numWorkers; i++ {
		wg.Add(1)
		go hashWorker(hashq, resultq, &wg)
	}

	for f := range resultq {
		hashed.paths[f.fullHash] = append(hashed.paths[f.fullHash], f)
	}
	hashedStore := hashed.Prune()
	if *summarize {
		fmt.Println(hashedStore.Summarize())
	}
}
