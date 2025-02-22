// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux
// +build linux

package fsnotify

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInotifyCloseRightAway(t *testing.T) {
	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher")
	}

	// Close immediately; it won't even reach the first unix.Read.
	w.Close()

	// Wait for the close to complete.
	<-time.After(50 * time.Millisecond)
	isWatcherReallyClosed(t, w)
}

func TestInotifyCloseSlightlyLater(t *testing.T) {
	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher")
	}

	// Wait until readEvents has reached unix.Read, and Close.
	<-time.After(50 * time.Millisecond)
	w.Close()

	// Wait for the close to complete.
	<-time.After(50 * time.Millisecond)
	isWatcherReallyClosed(t, w)
}

func TestInotifyCloseSlightlyLaterWithWatch(t *testing.T) {
	testDir := tempMkdir(t)
	defer os.RemoveAll(testDir)

	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher")
	}
	if err := w.Add(testDir); err != nil {
		t.Fatalf("Failed to add watcher to %s. %v", testDir, err)
	}

	// Wait until readEvents has reached unix.Read, and Close.
	<-time.After(50 * time.Millisecond)
	w.Close()

	// Wait for the close to complete.
	<-time.After(50 * time.Millisecond)
	isWatcherReallyClosed(t, w)
}

func TestInotifyCloseAfterRead(t *testing.T) {
	testDir := tempMkdir(t)
	defer os.RemoveAll(testDir)

	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher")
	}

	err = w.Add(testDir)
	if err != nil {
		t.Fatalf("Failed to add .")
	}

	// Generate an event.
	if _, err := os.Create(filepath.Join(testDir, "somethingSOMETHINGsomethingSOMETHING")); err != nil {
		t.Fatalf("Unable to create test file in %s. %v", testDir, err)
	}

	// Wait for readEvents to read the event, then close the watcher.
	<-time.After(50 * time.Millisecond)
	w.Close()

	// Wait for the close to complete.
	<-time.After(50 * time.Millisecond)
	isWatcherReallyClosed(t, w)
}

func isWatcherReallyClosed(t *testing.T, w *Watcher) {
	select {
	case err, ok := <-w.Errors:
		if ok {
			t.Fatalf("w.Errors is not closed; readEvents is still alive after closing (error: %v)", err)
		}
	default:
		t.Fatalf("w.Errors would have blocked; readEvents is still alive!")
	}

	select {
	case _, ok := <-w.Events:
		if ok {
			t.Fatalf("w.Events is not closed; readEvents is still alive after closing")
		}
	default:
		t.Fatalf("w.Events would have blocked; readEvents is still alive!")
	}
}

func TestInotifyCloseCreate(t *testing.T) {
	testDir := tempMkdir(t)
	defer os.RemoveAll(testDir)

	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer w.Close()

	err = w.Add(testDir)
	if err != nil {
		t.Fatalf("Failed to add testDir: %v", err)
	}
	h, err := os.Create(filepath.Join(testDir, "testfile"))
	if err != nil {
		t.Fatalf("Failed to create file in testdir: %v", err)
	}
	h.Close()
	select {
	case <-w.Events:
	case err := <-w.Errors:
		t.Fatalf("Error from watcher: %v", err)
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("Took too long to wait for event")
	}

	// At this point, we've received one event, so the goroutine is ready.
	// It's also blocking on unix.Read.
	// Now we try to swap the file descriptor under its nose.
	_ = w.Close()
	w, err = NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create second watcher: %v", err)
	}
	defer w.Close()

	<-time.After(50 * time.Millisecond)
	err = w.Add(testDir)
	if err != nil {
		t.Fatalf("Error adding testDir again: %v", err)
	}
}

// This test verifies the watcher can keep up with file creations/deletions
// when under load.
func TestInotifyStress(t *testing.T) {
	maxNumToCreate := 1000

	testDir := tempMkdir(t)
	defer os.RemoveAll(testDir)
	testFilePrefix := filepath.Join(testDir, "testfile")

	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer w.Close()

	err = w.Add(testDir)
	if err != nil {
		t.Fatalf("Failed to add testDir: %v", err)
	}

	doneChan := make(chan struct{})
	// The buffer ensures that the file generation goroutine is never blocked.
	errChan := make(chan error, 2*maxNumToCreate)

	go func() {
		for i := 0; i < maxNumToCreate; i++ {
			testFile := fmt.Sprintf("%s%d", testFilePrefix, i)

			handle, err := os.Create(testFile)
			if err != nil {
				errChan <- fmt.Errorf("Create failed: %v", err)
				continue
			}

			err = handle.Close()
			if err != nil {
				errChan <- fmt.Errorf("Close failed: %v", err)
				continue
			}
		}

		// If we delete a newly created file too quickly, inotify will skip the
		// create event and only send the delete event.
		time.Sleep(100 * time.Millisecond)

		for i := 0; i < maxNumToCreate; i++ {
			testFile := fmt.Sprintf("%s%d", testFilePrefix, i)
			err = os.Remove(testFile)
			if err != nil {
				errChan <- fmt.Errorf("Remove failed: %v", err)
			}
		}

		close(doneChan)
	}()

	creates := 0
	removes := 0

	finished := false
	after := time.After(10 * time.Second)
	for !finished {
		select {
		case <-after:
			t.Fatalf("Not done")
		case <-doneChan:
			finished = true
		case err := <-errChan:
			t.Fatalf("Got an error from file creator goroutine: %v", err)
		case err := <-w.Errors:
			t.Fatalf("Got an error from watcher: %v", err)
		case evt := <-w.Events:
			if !strings.HasPrefix(evt.Name, testFilePrefix) {
				t.Fatalf("Got an event for an unknown file: %s", evt.Name)
			}
			if evt.Op == Create {
				creates++
			}
			if evt.Op == Remove {
				removes++
			}
		}
	}

	// Drain remaining events from channels
	count := 0
	for count < 10 {
		select {
		case err := <-errChan:
			t.Fatalf("Got an error from file creator goroutine: %v", err)
		case err := <-w.Errors:
			t.Fatalf("Got an error from watcher: %v", err)
		case evt := <-w.Events:
			if !strings.HasPrefix(evt.Name, testFilePrefix) {
				t.Fatalf("Got an event for an unknown file: %s", evt.Name)
			}
			if evt.Op == Create {
				creates++
			}
			if evt.Op == Remove {
				removes++
			}
			count = 0
		default:
			count++
			// Give the watcher chances to fill the channels.
			time.Sleep(time.Millisecond)
		}
	}

	if creates-removes > 1 || creates-removes < -1 {
		t.Fatalf("Creates and removes should not be off by more than one: %d creates, %d removes", creates, removes)
	}
	if creates < 50 {
		t.Fatalf("Expected at least 50 creates, got %d", creates)
	}
}

func TestInotifyRemoveTwice(t *testing.T) {
	testDir := tempMkdir(t)
	defer os.RemoveAll(testDir)
	testFile := filepath.Join(testDir, "testfile")

	handle, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	handle.Close()

	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer w.Close()

	err = w.Add(testFile)
	if err != nil {
		t.Fatalf("Failed to add testFile: %v", err)
	}

	err = w.Remove(testFile)
	if err != nil {
		t.Fatalf("wanted successful remove but got: %v", err)
	}

	err = w.Remove(testFile)
	if err == nil {
		t.Fatalf("no error on removing invalid file")
	} else if !errors.Is(err, ErrNonExistentWatch) {
		t.Fatalf("unexpected error %v on removing invalid file", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.watches) != 0 {
		t.Fatalf("Expected watches len is 0, but got: %d, %v", len(w.watches), w.watches)
	}
	if len(w.paths) != 0 {
		t.Fatalf("Expected paths len is 0, but got: %d, %v", len(w.paths), w.paths)
	}
}

func TestInotifyInnerMapLength(t *testing.T) {
	testDir := tempMkdir(t)
	defer os.RemoveAll(testDir)
	testFile := filepath.Join(testDir, "testfile")

	handle, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	handle.Close()

	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	err = w.Add(testFile)
	if err != nil {
		t.Fatalf("Failed to add testFile: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for err := range w.Errors {
			t.Errorf("error received: %s", err)
		}
	}()

	err = os.Remove(testFile)
	if err != nil {
		t.Fatalf("Failed to remove testFile: %v", err)
	}
	<-w.Events                          // consume Remove event
	<-time.After(50 * time.Millisecond) // wait IN_IGNORE propagated

	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.watches) != 0 {
		t.Fatalf("Expected watches len is 0, but got: %d, %v", len(w.watches), w.watches)
	}
	if len(w.paths) != 0 {
		t.Fatalf("Expected paths len is 0, but got: %d, %v", len(w.paths), w.paths)
	}

	w.Close()
	wg.Wait()
}

func TestInotifyOverflow(t *testing.T) {
	// We need to generate many more events than the
	// fs.inotify.max_queued_events sysctl setting.
	// We use multiple goroutines (one per directory)
	// to speed up file creation.
	numDirs := 128
	numFiles := 1024

	testDir := tempMkdir(t)
	defer os.RemoveAll(testDir)

	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer w.Close()

	for dn := 0; dn < numDirs; dn++ {
		testSubdir := fmt.Sprintf("%s/%d", testDir, dn)

		err := os.Mkdir(testSubdir, 0o777)
		if err != nil {
			t.Fatalf("Cannot create subdir: %v", err)
		}

		err = w.Add(testSubdir)
		if err != nil {
			t.Fatalf("Failed to add subdir: %v", err)
		}
	}

	errChan := make(chan error, numDirs*numFiles)

	// All events need to be in the inotify queue before pulling events off it to trigger this error.
	wg := sync.WaitGroup{}
	for dn := 0; dn < numDirs; dn++ {
		testSubdir := fmt.Sprintf("%s/%d", testDir, dn)

		wg.Add(1)
		go func() {
			for fn := 0; fn < numFiles; fn++ {
				testFile := fmt.Sprintf("%s/%d", testSubdir, fn)

				handle, err := os.Create(testFile)
				if err != nil {
					errChan <- fmt.Errorf("Create failed: %v", err)
					continue
				}

				err = handle.Close()
				if err != nil {
					errChan <- fmt.Errorf("Close failed: %v", err)
					continue
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()

	creates := 0
	overflows := 0

	after := time.After(10 * time.Second)
	for overflows == 0 && creates < numDirs*numFiles {
		select {
		case <-after:
			t.Fatalf("Not done")
		case err := <-errChan:
			t.Fatalf("Got an error from file creator goroutine: %v", err)
		case err := <-w.Errors:
			if err == ErrEventOverflow {
				overflows++
			} else {
				t.Fatalf("Got an error from watcher: %v", err)
			}
		case evt := <-w.Events:
			if !strings.HasPrefix(evt.Name, testDir) {
				t.Fatalf("Got an event for an unknown file: %s", evt.Name)
			}
			if evt.Op == Create {
				creates++
			}
		}
	}

	if creates == numDirs*numFiles {
		t.Fatalf("Could not trigger overflow")
	}

	if overflows == 0 {
		t.Fatalf("No overflow and not enough creates (expected %d, got %d)",
			numDirs*numFiles, creates)
	}
}

func TestInotifyWatchList(t *testing.T) {
	testDir := tempMkdir(t)
	defer os.RemoveAll(testDir)
	testFile := filepath.Join(testDir, "testfile")

	handle, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	handle.Close()

	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer w.Close()

	err = w.Add(testFile)
	if err != nil {
		t.Fatalf("Failed to add testFile: %v", err)
	}
	err = w.Add(testDir)
	if err != nil {
		t.Fatalf("Failed to add testDir: %v", err)
	}

	value := w.WatchList()

	w.mu.Lock()
	defer w.mu.Unlock()
	for _, entry := range value {
		if _, ok := w.watches[entry]; !ok {
			t.Fatal("return value of WatchList is not same as the expected")
		}
	}
}

func TestINotifyNoBlockingSyscalls(t *testing.T) {
	getThreads := func() int {
		cmd := fmt.Sprintf("ls /proc/%d/task | wc -l", os.Getpid())
		output, err := exec.Command("/bin/bash", "-c", cmd).Output()
		if err != nil {
			t.Fatalf("Failed to execute command to check number of threads, err %s", err)
		}

		n, err := strconv.ParseInt(strings.Trim(string(output), "\n"), 10, 64)
		if err != nil {
			t.Fatalf("Failed to parse output as int, err: %s", err)
		}
		return int(n)
	}

	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	startingThreads := getThreads()
	// Call readEvents a bunch of times; if this function has a blocking raw syscall, it'll create many new kthreads
	for i := 0; i <= 60; i++ {
		go w.readEvents()
	}

	// Bad synchronization mechanism
	time.Sleep(time.Second * 2)

	endingThreads := getThreads()

	// Did we spawn any new threads?
	if diff := endingThreads - startingThreads; diff > 0 {
		t.Fatalf("Got a nonzero diff %v. starting: %v. ending: %v", diff, startingThreads, endingThreads)
	}
}
