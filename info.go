package main

import (
	"os"
	"path/filepath"
	"strings"
)

// return backup size including only the disk images so other scripts to put
// other stuff in the same folder without affecting the reported backup size
func BackupSize(backupName string) (uint64, error) {
	backupFolder := filepath.Join(Config.SMB.LocalPath, backupName)
	var size uint64
	err := filepath.Walk(
		backupFolder,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// identify disk images by extension
			isDiskImage := strings.EqualFold(
				filepath.Ext(path),
				".qcow2",
			)
			if isDiskImage && !info.IsDir() {
				size += uint64(info.Size())
			}
			return err
		},
	)
	return size, err
}
