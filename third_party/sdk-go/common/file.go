package common

import "os"

func Have(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func HaveDir(file string) bool {
	fi, err := os.Stat(file)
	return err == nil && fi.IsDir()
}

func EnsureDirExist(path string) error {
	if !HaveDir(path) {
		if err := os.MkdirAll(path, os.ModePerm); err != nil {
			return err
		}
	}
	return nil
}
