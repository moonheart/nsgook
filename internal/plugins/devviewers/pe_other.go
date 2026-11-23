//go:build !windows

package devviewers

import "os"

func readPEWindowsInfo(string, os.FileInfo) peWindowsInfo { return peWindowsInfo{} }
