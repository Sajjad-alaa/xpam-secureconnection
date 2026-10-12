package common

import (
	"compress/gzip"
	"io"
	"os"
)

func CompressToGzipFile(srcPath, dstPath string) error {
	sf, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer sf.Close()
	sfInfo, err := sf.Stat()
	if err != nil {
		return err
	}
	df, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer df.Close()
	writer := gzip.NewWriter(df)
	writer.Name = sfInfo.Name()
	writer.ModTime = sfInfo.ModTime()
	_, err = io.Copy(writer, sf)
	if err != nil {
		return err
	}
	return writer.Close()
}
