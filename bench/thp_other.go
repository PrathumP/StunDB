//go:build !linux

package main

func collapseHeap()    {}
func hugePagesKB() int { return -1 }
