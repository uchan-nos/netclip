package main

import (
	"os"
)

func getenvs(varnames ...string) string {
	for _, varname := range varnames {
		val := os.Getenv(varname)
		if val != "" {
			return val
		}
	}
	return ""
}

func guessUserName() string {
	return getenvs("LOGNAME", "USER", "LNAME", "USERNAME")
}

func guessHomeDir() string {
	dir := getenvs("HOME", "USERPROFILE")
	if dir == "" {
		homePath := os.Getenv("HOMEPATH")
		homeDrive := os.Getenv("HOMEDRIVE")
		dir = homeDrive + homePath
	}
	return dir
}
