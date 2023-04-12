# scale-backup
`scale-backup` is a utility for backing up VMs from a scale cluster. It is just a cli for the VirDomainExport API with the bare minimum of scheduling functionality I need. You will need to have an SMB share on the same computer as this utility for Scale to export VMs to. This is a 3rd party utility not endorsed by [Scale Computing](scalecomputing.com). I am personally running this on a [TrueNAS](https://www.truenas.com/) (FreeBSD) box, but you should be able to use it on Windows or Linux as well.

## Commands
### show-vms
This just prints a list of VMs in the cluster. It is primarily useful for scripting if you want to implement more complex backup logic than what is built-in.

### backup
This command takes 2 arguments
```
scale-backup backup <vm name> <backup name>
```

Scale exports consist of a folder with an XML file and some qcow2 images. This command will export the given VM to a new folder in the location configured in `config.toml`.

### restore
This command takes 2 arguments
```
scale-backup restore <backup name> <new vm name>
```

The backup name is the name of the folder (not full path) containing the backup.

### interactive-restore
This is like `scale-backup restore`, except that it takes no arguments and instead uses a menu system. This can only be used to restore scheduled backups (since it can tell which VM they came from)

### schedule
Run scheduled backups. This is intended to be run from `cron` or the Windows task scheduler. If the current time is outside the backup window specified in `config.toml` it will refuse to start.

### show-backups
List all backups and their size

### show-queue
Print a list of VMs that will be backed up when `schedule` is run. This list is in order of priority. VMs without a backup are first, followed by the VMs who's backups are the oldest.

## config.toml
`config.toml` is located at `C:\ProgramData\scale-backup\config.toml` on Windows or `/etc/scale-backup/config.toml` on other platforms.

### example config
```toml
[SMB]
Domain = 'CONTOSO' # optional, used by Scale to connect to the SMB share
Username = 'JohnDoe' # username Scale will use to connect to the SMB share
Password = 'pa$$w0rd' # password Scale will use to connect to the SMB share
Host = 'fileserver.contoso.com' # hostname or IP of this computer
ShareName = 'ServerBackups' # SMB share name
LocalPath = '/mnt/backups' # local path corresponding to ShareName

[Scale]
Username = 'admin' # username used to connect to the Scale API
Password = 'P@ssword' # password used to connect to the Scale API
Host = 'scale.cluster.local' # hostname or IP of a Scale node
CertFingerprint = 'FF::FF:FF...' # TLS certificate fingerprint (see below)

[SMTP]
# this section is optional
# SMTP server used for sending errors/alerts
# authentication is not supported
Host = 'smtp.office365.com'
Port = 25 # optional, default 25
From = 'scale-backups@contoso-corp.com'
To = 'ops-team@contoso-corp.com'

[Schedule]
# this section is optional
Tag = 'BackMeUp' # optional, if specified only back up VMs with this tag
Concurrency = 3 # max number of exports to run at once (Sacle's limit is 3)
StartTime = '5:00 PM' # start of the backup window
EndTime = '6:00 AM' # end of backup window
BackupInterval = '7 days' # how often should we back up a VM
# you can specify 1 or both of these options
MaxBackups = 7 # only keep this many backups
MaxAge = '30 days' # backups older than this will be deleted

[Hooks]
# you may add your own scripts here to be run before/after backups or
# before/after the schedule is run. {{Variables}} will be replaced. The
# ones in the examples below are all that is available for each hook.
# Note that this is NOT evaluated in a shell, so quoting is not required.
PreBackup = '/path/to/program {{VMName}} {{LocalPath}}/{{BackupName}}'
PostBackup = '/path/to/program {{VMName}} {{LocalPath}}/{{BackupName}}'
PreRestore = '/path/to/program {{NewVMName}} {{LocalPath}}/{{BackupName}}'
PostRestore = '/path/to/program {{NewVMName}} {{LocalPath}}/{{BackupName}}'
PreSchedule = '/path/to/program {{LocalPath}}'
PostSchedule = '/path/to/program {{LocalPath}}'
# Normally, in a scheduled run the PostBackup hook would run immediately
# after each backup, blocking other backups (based on Concurrency above)
# until the hook finished. If this is set to true, PostBackup hooks will be
# queued and run one-by-one, right before the PostSchedule hook.
DelayPostBackupWhenScheduled = false
```

### CertFingerprint
You are probably using self-signed certificates for Scale's admin interface. The API transmits the password using HTTP basic auth, so we really ought to validate the certificate somehow. My solution is to make you put the certificate fingerprint in the config. The easiest way to figure out what it should be is to leave it blank, then run `scale-backup` without any arguments. It will complain about not CertFingerprint not being set and will recommend a value:
```txt
Scale CertFingerprint not set
Detected fingerprints:
54:41:D0:CA:CC:75:DD:86:25:2E:DC:28:FC:25:CE:1D:D3:8B:F6:CB:E7:44:FF:4E:AA:A6:EC:65:D5:79:79:66 (Subject: localhost.localdomain)
```

### Hooks
Hooks let you prep your VMs to be backed up or process backups. For example: [I have one set up to convert the qcow2 disk images to VHDX disk images](hooks/convert-to-vhdx) so I can mount them from Windows to grab individual files. The template system is pretty minimal, since you're probably just going to use it to call a script anyway. It should be noted that the command string is not passed to a shell. Instead it is split on whitespace, with the first field being the program to be executed and all subsequent fields being passed as arguments. After splitting, `{{Variables}}` are replaced using simple string replacement. There are 2 side effects of this you might not be expecting:

1. There is no way to specify argument literals containing spaces. Sorry. If you need them, use a wrapper script.
2. You don't have to quote things, even if `{{Variable}}` might have a space in it.

### Schedule
You can use this together with something like `cron` to get a basic backup system. First, Set up `cron` to run `scale-backups schedule` at `StartTime` every day (it will fail if ran outside the backup window specified by `StartTime` and `EndTime`). Each time this is run, it will examine the list of VMs on the cluster and the list of local backups. Each VM who's backups are `BackupInterval` old will have a backup scheduled (limited by `Concurrency`). When the backup window closes (`EndTime`), currently running backups will be allowed to complete, but no more backups will be scheduled.

Cleanup happens at the end of the run. VM's with more than `MaxBackups` will have their oldest backups deleted. Any backups older than `MaxAge` will be deleted. Note: If you do not set `MaxAge`, backups for deleted VMs will need to be cleaned up manually.


## Tips
### DelayPostBackupWhenScheduled
If you have `PostBackup` hooks that don't need to run during the backup window, consider setting `DelayPostBackupWhenScheduled`. This will allow you to maximize the time during the backup window available for actually running backups.

### ZFS and Deduplication
You will really benefit from having a filesystem that can do deduplication. I use ZFS. Everyone on the internet says ZFS deduplication is terrible and will eat all your ram. What they don't tell you is you can fix this by changing `recordsize` to something larger. The default is 128KB, and each block in the deduplication table is roughly [320 bytes](https://www.oracle.com/technical-resources/articles/it-infrastructure/admin-o11-113-size-zfs-dedup.html). Thus the DDT for 1TB of storage (~8,388,608 blocks) is roughly 2.56GB. If you have enough ram for that, cool. I don't. There are 2 ways to improve things. You can give up on having the DDT in ram and instead put it on a really fast SSD (ideally in raid 1), or you can raise the block size so the DDT is smaller. Scale exports qcow2 images with a block size of 2MB. I recommend a matching `recordsize` of 2MB. It makes deduplication a little less efficient, but it brings the RAM requirement down to 160MB per TB of storage.

### VHDX
I have already mentioned I use a `PostBackup` hook to create a copy of my qcow2 images in fixed VHDX format (`-O vhdx -o subformat=fixed`). If you are running on ZFS with deduplication turned on, and are working primarily with Windows VMs, this can be really handy and won't cost you any space. Fixed VHDX files are a raw disk image with a header (footer?) at the end of the file. This means that each 2MB cluster from the qcow2 image will also be 2MB aligned in the fixed VHDX, and ZFS can dedupe the second copy down to almost nothing. Note that while this is practically free as far as disk space, there is still a CPU and IO cost to creating these images. See `DelayPostBackupWhenScheduled` to help deal with that.

The advantage to having a VHDX copy is that Windows can mount them natively, even over SMB. Just browse to the share and double click the VHDX file. If it contains an NTFS filesystem, you will be able to browse and recover individual files. Any modifications to the filesystem made this way will be persisted in the VHDX but will not affect the qcow2 image.

### zsh Auto-Completion
I don't claim to know what I am doing when it comes to zsh autocompletion, but I have this snippet I add to my `.zshrc` to give me basic autocompletion for `scale-backup`. You do need to manually put in the path to your backups.
```zsh
autoload -Uz compinit
compinit
function _scale-backup {
	local line state
	_arguments -C \
		'1: :(show-vms backup restore interactive-restore schedule show-backups show-queue)' \
		'2: :->arg2'
	case "$state" in
		arg2)
			case "$line[1]" in
				backup)
					local -a vms
					vms=("${(@f)$(scale-backup show-vms)}")
					compadd $vms
				;;
				restore)
					_files -/ -W /local/path/to/your/backups/
				;;
			esac
		;;
	esac
}
compdef _scale-backup scale-backup
```
