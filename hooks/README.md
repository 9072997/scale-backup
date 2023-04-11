This is a hook I use in my environment. It converts the qcow2 images Scale produces to VHDX files. It requires `qemu-img` to run. Configuring this hook might look like this:

```toml
[Hooks]
PostBackup = '/root/bin/convert-to-vhdx {{LocalPath}}/{{BackupName}}'
DelayPostBackupWhenScheduled = true
```
