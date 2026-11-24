//go:build !windows

package devviewers

import "os"

func readPEFileInfo(string, os.FileInfo) peWindowsInfo { return peWindowsInfo{} }

func verifyAuthenticode(string) string { return "Unavailable" }
