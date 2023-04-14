package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func ParseHookStr(hookStr string, variables map[string]string) (string, []string) {
	parts := strings.Fields(hookStr)
	if len(parts) == 0 {
		return "", nil
	}
	cmd := parts[0]

	// build args, replacing variables as we go
	args := make([]string, len(parts)-1)
	for i, arg := range parts[1:] {
		for k, v := range variables {
			arg = strings.ReplaceAll(arg, "{{"+k+"}}", v)
		}
		args[i] = arg
	}

	return cmd, args
}

func PreBackupHook(vmName, backupName string) error {
	if Config.Hooks.PreBackup == "" {
		return nil
	}

	cmd, args := ParseHookStr(
		Config.Hooks.PreBackup,
		map[string]string{
			"VMName":     vmName,
			"LocalPath":  Config.SMB.LocalPath,
			"BackupName": backupName,
		},
	)

	cmdObj := exec.Command(cmd, args...)
	cmdObj.Stdout = os.Stdout
	cmdObj.Stderr = os.Stderr
	err := cmdObj.Run()
	if err != nil {
		return fmt.Errorf(
			"pre-backup hook failed: %w",
			err,
		)
	}

	return nil
}

// if Config.Hooks.DelayPostBackupWhenScheduled is true, then we delay the
// post-backup hook until after all scheduled backups are done
var delayedHooks [][2]string

func PostBackupHook(vmName, backupName string, scheduled bool) error {
	if Config.Hooks.DelayPostBackupWhenScheduled && scheduled {
		delayedHooks = append(delayedHooks, [2]string{vmName, backupName})
		return nil
	}
	return postBackupHook(vmName, backupName)
}

// this one is wrapped because sometimes we want to delay it until after
// all scheduled backups are done
func postBackupHook(vmName, backupName string) error {
	if Config.Hooks.PostBackup == "" {
		return nil
	}

	cmd, args := ParseHookStr(
		Config.Hooks.PostBackup,
		map[string]string{
			"VMName":     vmName,
			"LocalPath":  Config.SMB.LocalPath,
			"BackupName": backupName,
		},
	)

	cmdObj := exec.Command(cmd, args...)
	cmdObj.Stdout = os.Stdout
	cmdObj.Stderr = os.Stderr
	err := cmdObj.Run()
	if err != nil {
		return fmt.Errorf(
			"post-backup hook failed: %w",
			err,
		)
	}

	return nil
}

func PreRestoreHook(newVMName, backupName string) error {
	if Config.Hooks.PreRestore == "" {
		return nil
	}

	cmd, args := ParseHookStr(
		Config.Hooks.PreRestore,
		map[string]string{
			"NewVMName":  newVMName,
			"LocalPath":  Config.SMB.LocalPath,
			"BackupName": backupName,
		},
	)

	cmdObj := exec.Command(cmd, args...)
	cmdObj.Stdout = os.Stdout
	cmdObj.Stderr = os.Stderr
	err := cmdObj.Run()
	if err != nil {
		return fmt.Errorf(
			"pre-restore hook failed: %w",
			err,
		)
	}

	return nil
}

func PostRestoreHook(newVMName, backupName string) error {
	if Config.Hooks.PostRestore == "" {
		return nil
	}

	cmd, args := ParseHookStr(
		Config.Hooks.PostRestore,
		map[string]string{
			"NewVMName":  newVMName,
			"LocalPath":  Config.SMB.LocalPath,
			"BackupName": backupName,
		},
	)

	cmdObj := exec.Command(cmd, args...)
	cmdObj.Stdout = os.Stdout
	cmdObj.Stderr = os.Stderr
	err := cmdObj.Run()
	if err != nil {
		return fmt.Errorf(
			"post-restore hook failed: %w",
			err,
		)
	}

	return nil
}

func PreScheduleHook() error {
	if Config.Hooks.PreSchedule == "" {
		return nil
	}

	cmd, args := ParseHookStr(
		Config.Hooks.PreSchedule,
		map[string]string{
			"LocalPath": Config.SMB.LocalPath,
		},
	)

	cmdObj := exec.Command(cmd, args...)
	cmdObj.Stdout = os.Stdout
	cmdObj.Stderr = os.Stderr
	err := cmdObj.Run()
	if err != nil {
		return fmt.Errorf(
			"pre-schedule hook failed: %w",
			err,
		)
	}

	return nil
}

func PostScheduleHook() error {
	// run delayed hooks
	var firstErr error
	for _, hook := range delayedHooks {
		err := postBackupHook(hook[0], hook[1])
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	delayedHooks = nil

	if Config.Hooks.PostSchedule == "" {
		return firstErr
	}

	cmd, args := ParseHookStr(
		Config.Hooks.PostSchedule,
		map[string]string{
			"LocalPath": Config.SMB.LocalPath,
		},
	)

	cmdObj := exec.Command(cmd, args...)
	cmdObj.Stdout = os.Stdout
	cmdObj.Stderr = os.Stderr
	err := cmdObj.Run()
	if err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}
