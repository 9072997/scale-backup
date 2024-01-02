package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/hyperjumptech/jiffy"
	"github.com/manifoldco/promptui"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/sync/semaphore"
)

func emailTerminalError(subject, bodyFormatString string, args ...interface{}) {
	bodyFormatString += "\n"
	fmt.Fprintln(os.Stderr, subject)
	fmt.Fprintf(os.Stderr, bodyFormatString, args...)
	Email(
		subject,
		fmt.Sprintf(bodyFormatString, args...),
	)
	os.Exit(1)
}

func ShowVMs() {
	vms, err := VMs("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// sort the list of VM names alphabetically
	vmNames := make([]string, 0, len(vms))
	for vmName := range vms {
		vmNames = append(vmNames, vmName)
	}
	sort.Strings(vmNames)

	for _, name := range vmNames {
		fmt.Println(name)
	}
}

func Backup(vmName, backupName string, scheduled bool) {
	DebugCall(vmName, backupName, scheduled)

	// run pre-backup hook
	err := PreBackupHook(vmName, backupName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Pre-backup hook failed: %s\n", err)
		Email(
			"Pre-backup hook failed",
			fmt.Sprintf(
				"Pre-backup hook failed for %s: %s",
				vmName,
				err,
			),
		)
	}

	// get a list of VMs and their UUIDs
	vms, err := VMs("")
	if err != nil {
		emailTerminalError(
			"Backup failed",
			"Backup of %s failed to start because the list of VMs could not be retrieved: %s",
			vmName,
			err,
		)
	}

	// get the UUID of the VM we're backing up
	vmUUID, exists := vms[vmName]
	if !exists {
		emailTerminalError(
			"Backup failed",
			"Backup of %s failed to start: VM not found",
			vmName,
		)
	}

	// start the backup and get the task tag to track it's progress
	taskTag, err := Export(vmUUID, backupName)
	if err != nil {
		emailTerminalError(
			"Backup failed",
			"Backup of %s failed to start: %s",
			vmName,
			err,
		)
	}

	if taskTag == "" {
		emailTerminalError(
			"Backup failed",
			"Backup of %s failed to start: no task tag returned",
			vmName,
		)
	}

	fmt.Printf("Backup of %s started as task %s\n", vmName, taskTag)

	errCount := 0
	percent := -2
	for {
		// get task status
		// if we fail to do this 5 times in a row, exit with error
		task, err := GetTask(taskTag)
		if err == nil {
			errCount = 0
		} else {
			errCount++
			if errCount > 5 {
				emailTerminalError(
					"Backup status unknown",
					"Cannot retrieve status of backup for %s: %s",
					vmName,
					err,
				)
			}
			time.Sleep(5 * time.Second)
			continue
		}
		// for errors
		taskJSON, _ := json.MarshalIndent(task, "", "\t")

		switch task.State {
		case "UNINITIALIZED":
			errCount++
			if errCount > 5 {
				emailTerminalError(
					"Backup failed to start",
					"Backup of %s failed to start\n%s",
					vmName,
					string(taskJSON),
				)
			}
		case "ERROR":
			emailTerminalError(
				"Backup failed",
				"Backup of %s failed\n%s",
				vmName,
				string(taskJSON),
			)
		case "QUEUED":
			errCount = 0
			if percent != -1 {
				percent = -1
				fmt.Println("Waiting for other tasks on the cluster to complete...")
			}
		case "RUNNING":
			errCount = 0
			if task.ProgressPercent != percent {
				percent = task.ProgressPercent
				fmt.Printf("%s: %d%% complete\n", vmName, percent)
			}
		case "COMPLETE":
			fmt.Printf("Backup of %s completed\n", vmName)
			// run post-backup hook
			err := PostBackupHook(vmName, backupName, scheduled)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Post-backup hook failed: %s\n", err)
				Email(
					"Post-backup hook failed",
					fmt.Sprintf(
						"Post-backup hook failed for %s: %s",
						vmName,
						err,
					),
				)
			}
			return
		default:
			errCount++
			if errCount > 5 {
				emailTerminalError(
					"Backup failed to start",
					"Unknown state for backup of %s\n%s",
					vmName,
					string(taskJSON),
				)
			}
		}
		time.Sleep(5 * time.Second)
	}
}

func Restore(backupName, newVMName string) {
	DebugCall(backupName, newVMName)

	// run pre-restore hook
	err := PreRestoreHook(newVMName, backupName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Pre-restore hook failed: %s\n", err)
		return
	}

	// check if the backup exists
	backupFolder := filepath.Join(Config.SMB.LocalPath, backupName)
	fileInfo, err := os.Stat(backupFolder)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Backup %s does not exist: %s\n", backupName, err)
		return
	}
	if !fileInfo.IsDir() {
		fmt.Fprintf(os.Stderr, "%s is not a directory\n", backupFolder)
		return
	}

	// start the backup and get the task tag to track it's progress
	taskTag, err := Import(newVMName, backupName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start: %s\n", err)
		return
	}

	if taskTag == "" {
		fmt.Fprintln(os.Stderr, "No task tag returned")
		return
	}

	fmt.Printf("Restore started as task %s\n", taskTag)

	errCount := 0
	percent := -2
	for {
		// get task status
		// if we fail to do this 5 times in a row, exit with error
		task, err := GetTask(taskTag)
		if err == nil {
			errCount = 0
		} else {
			errCount++
			if errCount > 5 {
				fmt.Fprintf(os.Stderr, "Cannot retrieve status of restore: %s\n", err)
				return
			}
			time.Sleep(5 * time.Second)
			continue
		}
		// for errors
		taskJSON, _ := json.MarshalIndent(task, "", "\t")

		switch task.State {
		case "UNINITIALIZED":
			errCount++
			if errCount > 5 {
				fmt.Fprintf(os.Stderr, "Restore failed to start\n%s\n", string(taskJSON))
				return
			}
		case "ERROR":
			fmt.Fprintf(os.Stderr, "Restore failed\n%s\n", string(taskJSON))
			return
		case "QUEUED":
			errCount = 0
			if percent != -1 {
				percent = -1
				fmt.Println("Waiting for other tasks on the cluster to complete...")
			}
		case "RUNNING":
			errCount = 0
			if task.ProgressPercent != percent {
				percent = task.ProgressPercent
				fmt.Printf("%d%% complete\n", percent)
			}
		case "COMPLETE":
			fmt.Printf("Restore of %s completed\n", newVMName)
			// run post-restore hook
			err := PostRestoreHook(newVMName, backupName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Post-restore hook failed: %s\n", err)
			}
			return
		default:
			errCount++
			if errCount > 5 {
				fmt.Fprintf(os.Stderr, "Unknown task state\n%s\n", string(taskJSON))
				return
			}
		}
		time.Sleep(5 * time.Second)
	}
}

func InteractiveRestore() {
	DebugCall()

	// get the list of backups
	backups, err := Backups()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get list of backups: %s\n", err)
		return
	}

	// prompt the user to select a VM
	var backedUpVMs []string
	for vmName := range backups {
		backedUpVMs = append(backedUpVMs, vmName)
	}
	sort.Strings(backedUpVMs)
	_, vmName, err := (&promptui.Select{
		Label: "Select a VM to restore",
		Items: backedUpVMs,
	}).Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to select VM: %s\n", err)
		return
	}

	// prompt the user to select a backup
	backupIdx, _, err := (&promptui.Select{
		Label: "Select a backup to restore",
		Items: backups[vmName],
	}).Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to select backup: %s\n", err)
		return
	}

	// get a list of VMs on the cluster to check for name conflicts
	existingVMs, err := VMs("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get list of VMs: %s\n", err)
		return
	}

	// prompt the user to enter a new VM name
	vmNameRegex := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	newVMName, err := (&promptui.Prompt{
		Label: "Enter a new name for the restored VM",
		Validate: func(input string) error {
			// can't be empty
			if input == "" {
				return errors.New("VM name cannot be empty")
			}
			// can't be the same as an existing VM
			_, exists := existingVMs[input]
			if exists {
				return errors.New("VM name already exists")
			}
			// can only contain letters, numbers, dashes, and underscores
			if !vmNameRegex.MatchString(input) {
				return errors.New("VM name can only contain letters, numbers, dashes, and underscores")
			}
			return nil
		},
	}).Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to select new VM name: %s\n", err)
		return
	}

	// prompt the user to confirm
	backupTimeStr := backups[vmName][backupIdx].Format("2006-01-02 03:04 PM (MST)")
	confirmed, err := (&promptui.Prompt{
		Label: fmt.Sprintf(
			"Restore %s from %s as %s",
			vmName,
			backupTimeStr,
			newVMName,
		),
		IsConfirm: true,
	}).Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to confirm restore: %s\n", err)
		return
	}
	if confirmed != "y" {
		fmt.Println("Restore cancelled")
		return
	}

	// run the restore
	backupName := DateTimePrefix(backups[vmName][backupIdx], vmName)
	Restore(backupName, newVMName)
}

func Schedule() {
	DebugCall()

	// check that schedule is configured
	if !ScheduleConfigured() {
		emailTerminalError(
			"Backup not started",
			"No backups started because the schedule is not configured",
		)
	}

	// check that we are in the backup window
	if !ScheduleIsActive() {
		emailTerminalError(
			"Backup not started",
			"No backups started because the backup window (%s-%s) is not active",
			Config.Schedule.StartTime,
			Config.Schedule.EndTime,
		)
	}

	// run the pre-schedule hook
	err := PreScheduleHook()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Pre-schedule hook failed: %s\n", err)
		Email(
			"Pre-schedule hook failed",
			fmt.Sprintf(
				"Pre-schedule hook failed: %s",
				err,
			),
		)
	}

	// already validated from when we validated the config
	backupInterval, err := jiffy.DurationOf(Config.Schedule.BackupInterval)
	if err != nil {
		panic(err)
	}

	// limit the number of concurrent backup jobs
	limiter := semaphore.NewWeighted(int64(Config.Schedule.Concurrency))
	for {
		limiter.Acquire(context.Background(), 1)

		// check that we are still in the backup window
		if !ScheduleIsActive() {
			fmt.Println("Backup window closed. Waiting for currently running backups to complete...")
			limiter.Release(1)
			break
		}

		// re-check the queue every time we are ready to start a new job
		queue, err := BackupQueue(backupInterval)
		if err != nil {
			emailTerminalError(
				"Backup not started",
				"Some (maybe all) backups skipped because the backup queue could not be retrieved: %s",
				err,
			)
		}

		// quit if the queue is empty
		if len(queue) == 0 {
			fmt.Println("No more backups in queue")
			limiter.Release(1)
			break
		}

		// start a backup job for the first VM in the queue
		vmName := queue[0]
		backupName := DateTimePrefix(time.Now(), vmName)
		go func(vmName, backupName string) {
			Backup(vmName, backupName, true)
			limiter.Release(1)
		}(vmName, backupName)

		// wait until we see the folder locally
		// this avoids starting 2 backups for the same VM
		for {
			_, err := os.Stat(filepath.Join(Config.SMB.LocalPath, backupName))
			if err == nil {
				break
			}
			if os.IsNotExist(err) {
				time.Sleep(time.Second)
				continue
			}
			emailTerminalError(
				"Backup failed",
				"Error while waiting for local file during backup of %s: %s",
				vmName,
				err,
			)
		}
	}

	// wait for all jobs to finish
	limiter.Acquire(context.Background(), int64(Config.Schedule.Concurrency))

	// already validated from when we validated the config
	tolerance, err := jiffy.DurationOf(Config.Schedule.Tolerance)
	if err != nil {
		panic(err)
	}
	tolerantInterval := backupInterval + tolerance

	// send email if there are still VMs in the queue
	queue, err := BackupQueue(tolerantInterval)
	if err != nil {
		// it would be odd to get an error here.
		// it only affects our ability to check if the queue is empty, so
		// don't send an email if we get an error.
		fmt.Fprintf(os.Stderr, "Error checking backup queue: %s\n", err)
		return
	}
	if len(queue) > 0 {
		var msg bytes.Buffer
		msg.WriteString("Backups are behind schedule.\n")
		msg.WriteString("The following VMs are still in the queue:\n\n")
		for _, vmName := range queue {
			fmt.Fprintf(&msg, "%s\n", vmName)
		}
		fmt.Fprintln(os.Stderr, msg.String())
		Email(
			"Backups are behind schedule",
			msg.String(),
		)
	}

	// cleanup old backups
	err = Cleanup()
	if err != nil {
		emailTerminalError(
			"Failed to cleanup old backups",
			"Error while trying to cleanup old backups: %s",
			err,
		)
	}

	// run the post-schedule hook
	err = PostScheduleHook()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Post-schedule hook failed: %s\n", err)
		Email(
			"Post-schedule hook failed",
			fmt.Sprintf(
				"Post-schedule hook failed: %s",
				err,
			),
		)
	}
}

func ShowBackups() {
	DebugCall()

	backups, err := Backups()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	// sort the list of VM names alphabetically
	vmNames := make([]string, 0, len(backups))
	for vmName := range backups {
		vmNames = append(vmNames, vmName)
	}
	sort.Strings(vmNames)

	for _, vmName := range vmNames {
		backupTimes := backups[vmName]
		fmt.Println(vmName)
		for _, backupTime := range backupTimes {
			name := DateTimePrefix(backupTime, vmName)
			size, err := BackupSize(name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting size of %s: %s\n", name, err)
			}
			backupTimeStr := backupTime.Format("2006-01-02 03:04 PM")
			fmt.Printf("\t%s (%s)\n", backupTimeStr, humanize.Bytes(size))
		}
	}
}

func ShowQueue() {
	DebugCall()

	// a queue requires a schedule to be configured
	if !ScheduleConfigured() {
		fmt.Fprintln(os.Stderr, "No schedule configured")
		return
	}

	// already validated from when we validated the config
	backupInterval, err := jiffy.DurationOf(Config.Schedule.BackupInterval)
	if err != nil {
		panic(err)
	}

	queue, err := BackupQueue(backupInterval)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	backups, err := Backups()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	for _, vmName := range queue {
		backups, backupExists := backups[vmName]
		if backupExists {
			lastBackup := backups[0]
			age := time.Since(lastBackup)
			fmt.Printf(
				"%s (%s old)\n",
				vmName,
				jiffy.DescribeDuration(age, &jiffy.Want{
					Year:      true,
					Month:     true,
					Day:       true,
					Hour:      true,
					Minute:    true,
					Second:    false,
					Verbose:   false,
					Separator: " ",
				}),
			)
		} else {
			fmt.Printf("%s (no backups)\n", vmName)
		}
	}
}

func UploadDiskMedia(filename string) {
	DebugCall()

	// open file
	file, err := os.Open(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open file %s: %s\n", filename, err)
		return
	}
	defer file.Close()

	// get file size
	fileInfo, err := file.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get file info for %s: %s\n", filename, err)
		return
	}
	fileSize := fileInfo.Size()

	// set up progress bar
	bar := progressbar.DefaultBytes(fileSize)
	reader := progressbar.NewReader(file, bar)

	// upload file
	basename := filepath.Base(filename)
	uuid, err := Upload(basename, fileSize, &reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to upload %s: %s\n", filename, err)
		return
	}

	fmt.Printf("Uploaded %s as %s\n", basename, uuid)
}

func main() {
	if len(os.Args) < 2 {
		basename := filepath.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, "Usage: %s <command> [args]\n", basename)
		fmt.Fprintln(os.Stderr, "Commands:")
		fmt.Fprintln(os.Stderr, "\tshow-vms")
		fmt.Fprintln(os.Stderr, "\tbackup <vm name> <backup name>")
		fmt.Fprintln(os.Stderr, "\trestore <backup name> <new vm name>")
		fmt.Fprintln(os.Stderr, "\tinteractive-restore")
		fmt.Fprintln(os.Stderr, "\tschedule")
		fmt.Fprintln(os.Stderr, "\tshow-backups")
		fmt.Fprintln(os.Stderr, "\tshow-queue")
		fmt.Fprintln(os.Stderr, "\tupload-disk-media <filename>")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "show-vms":
		ShowVMs()
	case "backup":
		if len(os.Args) != 4 {
			fmt.Fprintf(os.Stderr, "Usage: %s backup <vm name> <backup name>\n", os.Args[0])
			os.Exit(1)
		}
		Backup(os.Args[2], os.Args[3], false)
	case "restore":
		if len(os.Args) != 4 {
			fmt.Fprintf(os.Stderr, "Usage: %s restore <backup name> <new vm name>\n", os.Args[0])
			os.Exit(1)
		}
		Restore(os.Args[2], os.Args[3])
	case "interactive-restore":
		InteractiveRestore()
	case "schedule":
		Schedule()
	case "show-backups":
		ShowBackups()
	case "show-queue":
		ShowQueue()
	case "upload-disk-media":
		if len(os.Args) != 3 {
			fmt.Fprintf(os.Stderr, "Usage: %s upload-disk-media <filename>\n", os.Args[0])
			os.Exit(1)
		}
		UploadDiskMedia(os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
