package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hyperjumptech/jiffy"
)

// return true if we are currently in the scheduled backup window
func ScheduleIsActive() bool {
	// we already validated these when we validated the config
	start, err := time.Parse("3:04 PM", Config.Schedule.StartTime)
	if err != nil {
		panic(err)
	}
	end, err := time.Parse("3:04 PM", Config.Schedule.EndTime)
	if err != nil {
		panic(err)
	}

	// subtract 1 minute from the start time just in case whatever launches
	// this program is a little off
	start = start.Add(-1 * time.Minute)

	// current time without date or timezone
	now := time.Now()
	nowTime := time.Date(
		0, 1, 1,
		now.Hour(), now.Minute(), now.Second(), now.Nanosecond(),
		time.UTC,
	)

	if start.Before(end) {
		// if the schedule does not cross midnight
		return nowTime.After(start) && nowTime.Before(end)
	} else {
		// if the schedule does cross midnight
		return nowTime.After(start) || nowTime.Before(end)
	}
}

// take a VM name and return a folder name in the format
// "2006-01-02_15-04-05 My-VM-Name"
func DateTimePrefix(t time.Time, name string) string {
	return t.Format("2006-01-02_15-04-05 ") + name
}

// given a folder name in the format "2006-01-02_15-04-05 My-VM-Name"
// return the time and the name
func parseDateTime(fullName string) (time.Time, string, error) {
	prefix, name, _ := strings.Cut(fullName, " ")
	t, err := time.ParseInLocation("2006-01-02_15-04-05", prefix, time.Local)
	return t, name, err
}

// search local path for backups, listing each one for each VM
func Backups() (map[string][]time.Time, error) {
	entries, err := os.ReadDir(Config.SMB.LocalPath)
	if err != nil {
		return nil, err
	}

	backups := make(map[string][]time.Time)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t, name, err := parseDateTime(entry.Name())
		if err != nil {
			continue
		}
		backups[name] = append(backups[name], t)
	}

	// sort the backups for each VM (newest first)
	for _, times := range backups {
		sort.Slice(times, func(i, j int) bool {
			return times[i].After(times[j])
		})
	}

	return backups, nil
}

// list VMs that need to be backed up, sorted by priority
func BackupQueue(interval time.Duration) ([]string, error) {
	// get a list of all VMs
	vms, err := VMs(Config.Schedule.Tag)
	if err != nil {
		return nil, err
	}

	// get a list of all backups
	backups, err := Backups()
	if err != nil {
		return nil, err
	}

	// get backup ages for all VMs (MaxInt64 if never backed up)
	backupAges := make(map[string]time.Duration)
	for name := range vms {
		// if the VM has never been backed up
		if _, exists := backups[name]; !exists {
			backupAges[name] = time.Duration(math.MaxInt64)
			continue
		}

		lastBackup := backups[name][0]
		backupAges[name] = time.Since(lastBackup)
	}

	// list VMs to backup
	type vmToBackup struct {
		name string
		age  time.Duration
	}
	var vmsToBackup []vmToBackup
	for name, age := range backupAges {
		if age > interval {
			vmsToBackup = append(vmsToBackup, vmToBackup{name, age})
		}
	}

	// sort VMs to backup by age (oldest first)
	sort.Slice(vmsToBackup, func(i, j int) bool {
		return vmsToBackup[j].age < vmsToBackup[i].age
	})

	// return a list of VM names
	var vmNames []string
	for _, vm := range vmsToBackup {
		vmNames = append(vmNames, vm.name)
	}
	return vmNames, nil
}

// delete old backups
func Cleanup() error {
	backups, err := Backups()
	if err != nil {
		return err
	}

	maxAge := time.Duration(math.MaxInt64)
	if Config.Schedule.MaxAge != "" {
		// already validated from when we validated the config
		maxAge, err = jiffy.DurationOf(Config.Schedule.MaxAge)
		if err != nil {
			panic(err)
		}
	}

	maxBackups := math.MaxInt64
	if Config.Schedule.MaxBackups != 0 {
		maxBackups = Config.Schedule.MaxBackups
	}

	var backupsToDelete []string
	for vmName, backupTimes := range backups {
		for i, backupTime := range backupTimes {
			// if backup is too old
			if time.Since(backupTime) > maxAge {
				folderName := DateTimePrefix(backupTime, vmName)
				backupsToDelete = append(backupsToDelete, folderName)
			}

			// if there are too many backups
			if i >= maxBackups {
				folderName := DateTimePrefix(backupTime, vmName)
				backupsToDelete = append(backupsToDelete, folderName)
			}
		}
	}

	// sanity check that we are deleting less than 50% of backups
	deletionPercentage := 100 * float64(len(backupsToDelete)) / float64(len(backups))
	if deletionPercentage > 50 {
		return fmt.Errorf("refusing to delete %.0f%% of backups", deletionPercentage)
	}

	// delete backups
	for _, folderName := range backupsToDelete {
		err := os.RemoveAll(filepath.Join(Config.SMB.LocalPath, folderName))
		if err != nil {
			return err
		}
	}

	return nil
}
