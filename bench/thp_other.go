//go:build !linux

package main

func collapseHeap()       {}
func hugePagesKB() int    { return -1 }
func powerSource() string { return "unknown" }
