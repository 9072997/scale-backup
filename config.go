package main

import (
	"fmt"
	"net"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	tofu "github.com/9072997/golang-tofu"
	"github.com/hyperjumptech/jiffy"
	"github.com/pelletier/go-toml/v2"
)

var Config struct {
	SMB struct {
		Domain    string
		Username  string
		Password  string
		Host      string
		ShareName string
		LocalPath string
	}
	Scale struct {
		Username        string
		Password        string
		Host            string
		CertFingerprint string
	}
	SMTP struct {
		Host string
		Port int
		From string
		To   string
	}
	Schedule struct {
		Tag            string
		Concurrency    int
		StartTime      string
		EndTime        string
		BackupInterval string
		MaxBackups     int
		MaxAge         string
	}
	Hooks struct {
		PreBackup                    string
		PostBackup                   string
		PreRestore                   string
		PostRestore                  string
		PreSchedule                  string
		PostSchedule                 string
		DelayPostBackupWhenScheduled bool
	}
}

func isZero(x any) bool {
	return reflect.DeepEqual(x, reflect.Zero(reflect.TypeOf(x)).Interface())
}

func SMTPConfigured() bool {
	return !isZero(Config.SMTP.Host)
}

func ScheduleConfigured() bool {
	return !isZero(Config.Schedule.Tag)
}

// try in order:
// 1. SCALE_BACKUP_CONFIG environment variable
// 2. ./scale-backup.toml
// 3. ~/.scale-backup.toml (windows: %APPDATA%/scale-backup.toml)
// 4. /etc/scale-backup.toml (windows: %ProgramData%/scale-backup.toml)
func findConfigFile() string {
	configFile := os.Getenv("SCALE_BACKUP_CONFIG")
	if configFile != "" {
		return configFile
	}

	configFile = "scale-backup.toml"
	_, err := os.Stat(configFile)
	if err == nil {
		return "scale-backup.toml"
	}

	if runtime.GOOS == "windows" {
		configFile = filepath.Join(os.Getenv("APPDATA"), "scale-backup.toml")
	} else {
		configFile = filepath.Join(os.Getenv("HOME"), ".scale-backup.toml")
	}
	_, err = os.Stat(configFile)
	if err == nil {
		return configFile
	}

	if runtime.GOOS == "windows" {
		configFile = filepath.Join(os.Getenv("ProgramData"), "scale-backup.toml")
	} else {
		configFile = "/etc/scale-backup.toml"
	}
	_, err = os.Stat(configFile)
	if err == nil {
		return configFile
	}

	return ""
}

func init() {
	configFile := findConfigFile()
	if configFile == "" {
		fmt.Fprintln(os.Stderr, "Config file not found")
		fmt.Fprintln(os.Stderr, "The following locations were searched in order:")
		fmt.Fprintln(os.Stderr, "  1. SCALE_BACKUP_CONFIG environment variable")
		fmt.Fprintln(os.Stderr, "  2. ./scale-backup.toml")
		fmt.Fprintln(os.Stderr, "  3. ~/.scale-backup.toml (windows: %APPDATA%/scale-backup.toml)")
		fmt.Fprintln(os.Stderr, "  4. /etc/scale-backup.toml (windows: %ProgramData%/scale-backup.toml)")

		// fill out config with demo values and print to stderr as an example
		Config.SMB.Domain = "CONTOSO"
		Config.SMB.Username = "JohnDoe"
		Config.SMB.Password = "pa$$w0rd"
		Config.SMB.Host = "fileserver.contoso.com"
		Config.SMB.ShareName = "ServerBackups"
		Config.SMB.LocalPath = "/mnt/backups"
		Config.Scale.Username = "admin"
		Config.Scale.Password = "P@ssword"
		Config.Scale.Host = "scale.cluster.local"
		Config.Scale.CertFingerprint = "FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF"
		Config.SMTP.Host = "smtp.office365.com"
		Config.SMTP.Port = 25
		Config.SMTP.From = "scale-backups@contoso-corp.com"
		Config.SMTP.To = "ops-team@contoso-corp.com"
		Config.Schedule.Tag = "BackMeUp"
		Config.Schedule.Concurrency = 3
		Config.Schedule.StartTime = "5:00 PM"
		Config.Schedule.EndTime = "6:00 AM"
		Config.Schedule.BackupInterval = "7 days"
		Config.Schedule.MaxBackups = 7
		Config.Schedule.MaxAge = "30 days"
		Config.Hooks.PreBackup = "/path/to/program {{VMName}} {{LocalPath}}/{{BackupName}}"
		Config.Hooks.PostBackup = "/path/to/program {{VMName}} {{LocalPath}}/{{BackupName}}"
		Config.Hooks.PreRestore = "/path/to/program {{NewVMName}} {{LocalPath}}/{{BackupName}}"
		Config.Hooks.PostRestore = "/path/to/program {{NewVMName}} {{LocalPath}}/{{BackupName}}"
		Config.Hooks.PreSchedule = "/path/to/program {{LocalPath}}"
		Config.Hooks.PostSchedule = "/path/to/program {{LocalPath}}"
		Config.Hooks.DelayPostBackupWhenScheduled = false

		tomlBytes, err := toml.Marshal(Config)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(os.Stderr, "Example config:\n%s\n", string(tomlBytes))

		os.Exit(1)
	}

	configStr, err := os.ReadFile(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config file: %s\n", err)
		os.Exit(1)
	}

	err = toml.Unmarshal(configStr, &Config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing config file: %s\n", err)
		os.Exit(1)
	}

	// validate config

	// check required fields are present
	if Config.SMB.Username == "" {
		fmt.Fprintln(os.Stderr, "SMB Username not set")
		os.Exit(1)
	}
	if Config.SMB.Password == "" {
		fmt.Fprintln(os.Stderr, "SMB Password not set")
		os.Exit(1)
	}
	if Config.SMB.Host == "" {
		fmt.Fprintln(os.Stderr, "SMB Host not set")
		os.Exit(1)
	}
	if Config.SMB.ShareName == "" {
		fmt.Fprintln(os.Stderr, "SMB ShareName not set")
		os.Exit(1)
	}
	if Config.SMB.LocalPath == "" {
		fmt.Fprintln(os.Stderr, "SMB LocalPath not set")
		os.Exit(1)
	}
	if Config.Scale.Username == "" {
		fmt.Fprintln(os.Stderr, "Scale Username not set")
		os.Exit(1)
	}
	if Config.Scale.Password == "" {
		fmt.Fprintln(os.Stderr, "Scale Password not set")
		os.Exit(1)
	}
	if Config.Scale.Host == "" {
		fmt.Fprintln(os.Stderr, "Scale Host not set")
		os.Exit(1)
	}
	if Config.Scale.CertFingerprint == "" {
		fmt.Fprintln(os.Stderr, "Scale CertFingerprint not set")
		// show the fingerprint to the user
		certs, err := tofu.GetFingerprints(Config.Scale.Host)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting scale certificates: %s\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Detected fingerprints:")
		for _, cert := range certs {
			fmt.Fprintf(os.Stderr, "\t%s (%s)\n", cert.Fingerprint, cert.Subject)
		}
		os.Exit(1)
	}

	// Config.Schedule is optional, but if it is present, validate it
	if ScheduleConfigured() {
		if Config.Schedule.StartTime == "" {
			fmt.Fprintln(os.Stderr, "Schedule StartTime not set")
			os.Exit(1)
		}
		if Config.Schedule.EndTime == "" {
			fmt.Fprintln(os.Stderr, "Schedule EndTime not set")
			os.Exit(1)
		}
		if Config.Schedule.BackupInterval == "" {
			fmt.Fprintln(os.Stderr, "Schedule BackupInterval not set")
			os.Exit(1)
		}

		// concurrency must be 1, 2, or 3
		switch Config.Schedule.Concurrency {
		case 0:
			// default to 3
			Config.Schedule.Concurrency = 3
		case 1, 2, 3:
			// valid
		default:
			fmt.Fprintln(os.Stderr, "Schedule Concurrency must be 1, 2, or 3")
			os.Exit(1)
		}

		// at least one out of MaxBackups and MaxAge must be set
		if Config.Schedule.MaxBackups == 0 && Config.Schedule.MaxAge == "" {
			fmt.Fprintln(os.Stderr, "Neither MaxBackups nor MaxAge is set")
			os.Exit(1)
		}

		// if MaxAge is not set we will never delete backups from deleted VMs
		if Config.Schedule.MaxAge == "" {
			fmt.Fprintln(os.Stderr, "WARNING: Schedule MaxAge not set. Backups from deleted VMs will never be deleted.")
		} else {
			// MaxAge should be a valid duration
			_, err = jiffy.DurationOf(Config.Schedule.MaxAge)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Schedule MaxAge is not a valid duration")
				os.Exit(1)
			}
		}

		// start time and end time should be valid times
		_, err = time.ParseInLocation("3:04 PM", Config.Schedule.StartTime, time.Local)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Schedule StartTime is not a valid time")
			os.Exit(1)
		}
		_, err = time.ParseInLocation("3:04 PM", Config.Schedule.EndTime, time.Local)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Schedule EndTime is not a valid time")
			os.Exit(1)
		}

		// backup interval should be a valid duration
		_, err = jiffy.DurationOf(Config.Schedule.BackupInterval)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Schedule BackupInterval is not a valid duration")
			os.Exit(1)
		}
	}

	// DelayPostBackupWhenScheduled only makes sense if PostBackup is set
	if Config.Hooks.DelayPostBackupWhenScheduled && Config.Hooks.PostBackup == "" {
		fmt.Fprintln(os.Stderr, "DelayPostBackupWhenScheduled is set but PostBackup is not. There is nothing to delay.")
		os.Exit(1)
	}

	// validate SMTP settings if they are set
	if SMTPConfigured() {
		// host is required
		if Config.SMTP.Host == "" {
			fmt.Fprintln(os.Stderr, "SMTP Host not set")
			os.Exit(1)
		}

		// default to 25 if port is not set
		if Config.SMTP.Port == 0 {
			Config.SMTP.Port = 25
		}

		// FROM and TO addresses should be valid email addresses
		_, err = mail.ParseAddress(Config.SMTP.From)
		if err != nil {
			fmt.Fprintln(os.Stderr, "SMTP From is not a valid email address")
			os.Exit(1)
		}
		_, err = mail.ParseAddress(Config.SMTP.To)
		if err != nil {
			fmt.Fprintln(os.Stderr, "SMTP To is not a valid email address")
			os.Exit(1)
		}
	} else {
		fmt.Fprintln(os.Stderr, "WARNING: SMTP is not configured. No email notifications will be sent.")
	}

	// share name should not contain any slashes
	if strings.Contains(Config.SMB.ShareName, "/") {
		fmt.Fprintln(os.Stderr, "SMB ShareName should not contain slashes")
		os.Exit(1)
	}
	if strings.Contains(Config.SMB.ShareName, `\`) {
		fmt.Fprintln(os.Stderr, "SMB ShareName should not contain backslashes")
		os.Exit(1)
	}

	// local path should exist and be a directory
	fileInfo, err := os.Stat(Config.SMB.LocalPath)
	if os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "SMB LocalPath does not exist")
		os.Exit(1)
	}
	if !fileInfo.IsDir() {
		fmt.Fprintln(os.Stderr, "SMB LocalPath is not a directory")
		os.Exit(1)
	}

	// all 3 hosts should be resolvable
	_, err = net.LookupIP(Config.SMB.Host)
	if err != nil {
		fmt.Fprintln(os.Stderr, "SMB Host is not resolvable")
		os.Exit(1)
	}
	_, err = net.LookupIP(Config.Scale.Host)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Scale Host is not resolvable")
		os.Exit(1)
	}
	if Config.SMTP.Host != "" {
		_, err = net.LookupIP(Config.SMTP.Host)
		if err != nil {
			fmt.Fprintln(os.Stderr, "SMTP Host is not resolvable")
			os.Exit(1)
		}
	}

	// make sure we can construct a HTTP client from the cert fingerprint
	_, err = tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating HTTP client from cert fingerprint: %s\n", err)
		os.Exit(1)
	}

	// make sure we can find the executables mentioned in the hooks
	if Config.Hooks.PreBackup != "" {
		cmd := strings.Fields(Config.Hooks.PreBackup)[0]
		_, err = exec.LookPath(cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding pre-backup hook: %s\n", err)
			os.Exit(1)
		}
	}
	if Config.Hooks.PostBackup != "" {
		cmd := strings.Fields(Config.Hooks.PostBackup)[0]
		_, err = exec.LookPath(cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding post-backup hook: %s\n", err)
			os.Exit(1)
		}
	}
	if Config.Hooks.PreRestore != "" {
		cmd := strings.Fields(Config.Hooks.PreRestore)[0]
		_, err = exec.LookPath(cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding pre-restore hook: %s\n", err)
			os.Exit(1)
		}
	}
	if Config.Hooks.PostRestore != "" {
		cmd := strings.Fields(Config.Hooks.PostRestore)[0]
		_, err = exec.LookPath(cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding post-restore hook: %s\n", err)
			os.Exit(1)
		}
	}
	if Config.Hooks.PreSchedule != "" {
		cmd := strings.Fields(Config.Hooks.PreSchedule)[0]
		_, err = exec.LookPath(cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding pre-schedule hook: %s\n", err)
			os.Exit(1)
		}
	}
	if Config.Hooks.PostSchedule != "" {
		cmd := strings.Fields(Config.Hooks.PostSchedule)[0]
		_, err = exec.LookPath(cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding post-schedule hook: %s\n", err)
			os.Exit(1)
		}
	}
}
