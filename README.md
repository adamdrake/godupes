# CAUTION

This project was created as a conceptual prototype and is not intended to be used for any destructive operations on files.  It is untested, unfinished, and unsupported in every way.  Use at your own risk.

# Overview

Godupes is a duplicate file detector in the spirit of `fdupes` or `jdupes`.  It detects duplicates with a multi-tiered approach in order to avoid unnecessarily expensive hashing of entire files.

Note that the current version does not hash files as a stream of bytes directly from disk but rather reads the file into RAM and then hashes.  This may be corrected in the future but for this prototype it was not necessary.  An additional benefit from such a correction would be to allow the use of a -lowmem=X flag that would support restricting memory use when required.

## Algorithm overview

First, walk the directory tree and skip anything which isn't a file.

For files, read the first X bytes of a file and the total file size in addition to the path.  Hash this X bytes and use it as the key in a map from the byte hashes to slices of Files.  Call this bytesMatch.  Delete the elements of bytesMatch which contain only one element as these are unique files with no duplicates.

Now iterate through bytesMatch and hash each file, using the output of the hash as they key in a map from hashes to files.  This resulting map will have all sets of files which hash to the same value and therefore can be taken to be identical.

## Algorithm comparison

The process of first screening files by the first bytes of the files themselves serves to filter out a large number of files which we do not need to hash, thus saving CPU time.  If the first X bytes of the files are not the same, then they are clearly not duplicate files.

*fdupes* hashes the files and also does byte-wise comparison of files detected as duplicates, so each file has to be read twice.

*fmlint* uses a default of 160bit SHA-1 hash to detect duplicate files.

For memory usage, *dupd* uses a SQLite database if it runs out of memory, while *rmlint* uses a path tree structure to reduce memory usage.  In our case, the total maximum memory usage before the hashing step is 4kb for every file in the directory tree.