# File system notifications for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/shogo82148/fsnotify.svg)](https://pkg.go.dev/github.com/shogo82148/fsnotify) [![Go Report Card](https://goreportcard.com/badge/github.com/shogo82148/fsnotify)](https://goreportcard.com/report/github.com/shogo82148/fsnotify)

fsnotify utilizes [`golang.org/x/sys`](https://pkg.go.dev/golang.org/x/sys) rather than [`syscall`](https://pkg.go.dev/syscall) from the standard library.

Cross platform: Windows, Linux, BSD and macOS.

| Adapter               | OS                               | Status                                                       |
| --------------------- | -------------------------------- | ------------------------------------------------------------ |
| inotify               | Linux 2.6.27 or later, Android\* | Supported                                                    |
| kqueue                | BSD, macOS, iOS\*                | Supported                                                    |
| ReadDirectoryChangesW | Windows                          | Supported                                                    |
| FSEvents              | macOS                            | [Planned](https://github.com/fsnotify/fsnotify/issues/11)    |
| FEN                   | Solaris 11                       | [In Progress](https://github.com/fsnotify/fsnotify/pull/371) |
| fanotify              | Linux 2.6.37+                    | [Maybe](https://github.com/fsnotify/fsnotify/issues/114)     |
| USN Journals          | Windows                          | [Maybe](https://github.com/fsnotify/fsnotify/issues/53)      |
| Polling               | _All_                            | [Maybe](https://github.com/fsnotify/fsnotify/issues/9)       |

\* Android and iOS are untested.

Please see [the documentation](https://pkg.go.dev/github.com/shogo82148/fsnotify) and consult the [FAQ](#faq) for usage information.

## API stability

fsnotify is a fork of [howeyc/fsnotify](https://github.com/howeyc/fsnotify) with a new API as of v1.0. The API is based on [this design document](http://goo.gl/MrYxyA).

All [releases](https://github.com/shogo82148/fsnotify/releases) are tagged based on [Semantic Versioning](http://semver.org/).

## Usage

```go
package main

import (
	"log"

	"github.com/shogo82148/fsnotify"
)

func main() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				log.Println("event:", event)
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Println("modified file:", event.Name)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}
	}()

	err = watcher.Add("/tmp/foo")
	if err != nil {
		log.Fatal(err)
	}
	<-done
}
```

## Contributing

Please refer to [CONTRIBUTING][] before opening an issue or pull request.

## FAQ

**When a file is moved to another directory is it still being watched?**

No (it shouldn't be, unless you are watching where it was moved to).

**When I watch a directory, are all subdirectories watched as well?**

No, you must add watches for any directory you want to watch (a recursive watcher is on the roadmap [#18][]).

**Do I have to watch the Error and Event channels in a separate goroutine?**

As of now, yes. Looking into making this single-thread friendly (see [howeyc #7][#7])

**Why am I receiving multiple events for the same file on OS X?**

Spotlight indexing on OS X can result in multiple events (see [howeyc #62][#62]). A temporary workaround is to add your folder(s) to the _Spotlight Privacy settings_ until we have a native FSEvents implementation (see [#11][]).

**How many files can be watched at once?**

There are OS-specific limits as to how many watches can be created:

- Linux: /proc/sys/fs/inotify/max_user_watches contains the limit, reaching this limit results in a "no space left on device" error.
- BSD / OSX: sysctl variables "kern.maxfiles" and "kern.maxfilesperproc", reaching these limits results in a "too many open files" error.

**Why don't notifications work with NFS filesystems or filesystem in userspace (FUSE)?**

fsnotify requires support from underlying OS to work. The current NFS protocol does not provide network level support for file notifications.

[#62]: https://github.com/howeyc/fsnotify/issues/62
[#18]: https://github.com/fsnotify/fsnotify/issues/18
[#11]: https://github.com/fsnotify/fsnotify/issues/11
[#7]: https://github.com/howeyc/fsnotify/issues/7
[contributing]: https://github.com/shogo82148/fsnotify/blob/main/CONTRIBUTING.md

## Related Projects

- [notify](https://github.com/rjeczalik/notify)
- [fsevents](https://github.com/fsnotify/fsevents)
