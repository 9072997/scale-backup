package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	tofu "github.com/9072997/golang-tofu"
)

type VM struct {
	UUID        string     `json:"uuid"`
	Name        string     `json:"name"`
	Tags        string     `json:"tags"`
	IsTransient bool       `json:"isTransient"`
	BlockDevs   []BlockDev `json:"blockDevs"`
}

type BlockDev struct {
	UUID                string `json:"uuid"`
	VirDomainUUID       string `json:"virDomainUUID"`
	Type                string `json:"type"`
	Capacity            int    `json:"capacity"`
	Allocation          int    `json:"allocation"`
	DisableSnapshotting bool   `json:"disableSnapshotting"`
}

type Snapshot struct {
	DomainUUID                string `json:"domainUUID"`
	Label                     string `json:"label"`
	Type                      string `json:"type"`
	LocalRetainUntilTimestamp int64  `json:"localRetainUntilTimestamp"`
	Replication               bool   `json:"replication"`
}

type DiskFromSnapshotOpts struct {
	Options struct {
		RegenerateDiskID bool `json:"regenerateDiskID"`
	} `json:"options"`
	SnapUUID string `json:"snapUUID"`
	Template struct {
		VirDomainUUID string `json:"virDomainUUID"`
		Type          string `json:"type"`
		Capacity      int    `json:"capacity"`
	} `json:"template"`
}

type Task struct {
	// State can be: UNINITIALIZED, QUEUED, RUNNING, COMPLETE, or ERROR
	State                 string   `json:"state"`
	TaskTag               string   `json:"taskTag"`
	ProgressPercent       int      `json:"progressPercent"`
	FormattedDescription  string   `json:"formattedDescription"`
	DescriptionParameters []string `json:"descriptionParameters"`
	FormattedMessage      string   `json:"formattedMessage"`
	MessageParameters     []any    `json:"messageParameters"`
	CreatedUUID           string   `json:"createdUUID"`
}

type ExportOptions struct {
	Target struct {
		PathURI                  string `json:"pathURI"`
		Format                   string `json:"format"`
		Compress                 bool   `json:"compress"`
		AllowNonSequentialWrites bool   `json:"allowNonSequentialWrites"`
		ParallelCountPerTransfer int    `json:"parallelCountPerTransfer"`
	} `json:"target"`
}

type ImportOptions struct {
	Source struct {
		PathURI                  string `json:"pathURI"`
		Format                   string `json:"format"`
		AllowNonSequentialWrites bool   `json:"allowNonSequentialWrites"`
		ParallelCountPerTransfer int    `json:"parallelCountPerTransfer"`
	} `json:"source"`
	Template struct {
		Name string `json:"name"`
	} `json:"template"`
}

func VMs(searchTag string) (map[string]string, error) {
	debugReturn := DebugCall(searchTag)

	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   Config.Scale.Host,
		Path:   "/rest/v1/VirDomain",
	}
	req, err := http.NewRequest("GET", apiURL.String(), nil)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	resp, err := DebugHTTP(client, req)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	defer resp.Body.Close()
	var vms []VM
	err = json.NewDecoder(resp.Body).Decode(&vms)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}

	vmMap := make(map[string]string)
	for _, vm := range vms {
		// skip transient VMs
		if vm.IsTransient {
			continue
		}

		// if a search tag was specified, but this VM doesn't have it
		// skip the VM
		if searchTag != "" {
			tags := strings.Split(vm.Tags, ",")
			found := false
			for _, tag := range tags {
				if tag == searchTag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		vmMap[vm.Name] = vm.UUID
	}

	debugReturn(vmMap, nil)
	return vmMap, nil
}

func VMDisks(vmUUID string) ([]BlockDev, error) {
	debugReturn := DebugCall(vmUUID)

	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   Config.Scale.Host,
		Path:   "/rest/v1/VirDomain/" + url.PathEscape(vmUUID),
	}
	req, err := http.NewRequest("GET", apiURL.String(), nil)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	resp, err := DebugHTTP(client, req)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	defer resp.Body.Close()
	var vms []VM
	err = json.NewDecoder(resp.Body).Decode(&vms)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}

	if len(vms) != 1 {
		err := fmt.Errorf("expected 1 VM, got %d", len(vms))
		debugReturn(nil, err)
		return nil, err
	}

	debugReturn(vms[0].BlockDevs, nil)
	return vms[0].BlockDevs, nil
}

func Disks() ([]BlockDev, error) {
	debugReturn := DebugCall()

	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   Config.Scale.Host,
		Path:   "/rest/v1/VirDomain",
	}
	req, err := http.NewRequest("GET", apiURL.String(), nil)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	resp, err := DebugHTTP(client, req)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	defer resp.Body.Close()
	var vms []VM
	err = json.NewDecoder(resp.Body).Decode(&vms)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}

	var disks []BlockDev
	for _, vm := range vms {
		disks = append(disks, vm.BlockDevs...)
	}

	debugReturn(disks, nil)
	return disks, nil
}

func CreateSnapshot(vmUUID, snapshotName string, duration time.Duration) (*Task, error) {
	debugReturn := DebugCall(vmUUID, snapshotName, duration)

	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   Config.Scale.Host,
		Path:   "/rest/v1/VirDomainSnapshot",
	}
	expireTime := time.Now().Add(duration).Unix()
	reqBody, err := json.Marshal(Snapshot{
		DomainUUID:                vmUUID,
		Label:                     snapshotName,
		Type:                      "AUTOMATED",
		LocalRetainUntilTimestamp: expireTime,
		Replication:               false,
	})
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	req, err := http.NewRequest("POST", apiURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := DebugHTTP(client, req)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	defer resp.Body.Close()
	var task Task
	err = json.NewDecoder(resp.Body).Decode(&task)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	debugReturn(task, nil)
	return &task, nil
}

func DiskFromSnapshot(src BlockDev, snap, dstVMUUID string) (string, error) {
	debugReturn := DebugCall(src)

	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   Config.Scale.Host,
		Path:   "/rest/v1/VirDomainBlockDevice/" + url.PathEscape(src.UUID) + "/clone",
	}
	var opts DiskFromSnapshotOpts
	opts.Options.RegenerateDiskID = false
	opts.SnapUUID = snap
	opts.Template.VirDomainUUID = dstVMUUID
	opts.Template.Type = src.Type
	opts.Template.Capacity = src.Capacity
	reqBody, err := json.Marshal(opts)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	req, err := http.NewRequest("POST", apiURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := DebugHTTP(client, req)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	defer resp.Body.Close()
	var task Task
	err = json.NewDecoder(resp.Body).Decode(&task)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	debugReturn(task.TaskTag, nil)
	return task.TaskTag, nil
}

func GetTask(taskTag string) (*Task, error) {
	debugReturn := DebugCall(taskTag)

	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   Config.Scale.Host,
		Path:   "/rest/v1/TaskTag/" + url.PathEscape(taskTag),
	}
	req, err := http.NewRequest("GET", apiURL.String(), nil)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	resp, err := DebugHTTP(client, req)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}
	defer resp.Body.Close()
	var tasks []Task
	err = json.NewDecoder(resp.Body).Decode(&tasks)
	if err != nil {
		debugReturn(nil, err)
		return nil, err
	}

	if len(tasks) != 1 {
		err := fmt.Errorf("expected 1 task, got %d", len(tasks))
		debugReturn(nil, err)
		return nil, err
	}
	debugReturn(&tasks[0], nil)
	return &tasks[0], nil
}

func Export(vmUUID, folder string) (string, error) {
	debugReturn := DebugCall(vmUUID, folder)

	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   Config.Scale.Host,
		Path:   "/rest/v1/VirDomain/" + url.PathEscape(vmUUID) + "/export",
	}
	var exportOptions ExportOptions
	exportOptions.Target.PathURI = (&url.URL{
		Scheme: "smb",
		User: url.UserPassword(
			smbUserAndDomain(),
			Config.SMB.Password,
		),
		Host: Config.SMB.Host,
		Path: path.Join("/", Config.SMB.ShareName, folder),
	}).String()
	exportOptions.Target.Format = "qcow2"
	exportOptions.Target.Compress = false
	exportOptions.Target.AllowNonSequentialWrites = true
	exportOptions.Target.ParallelCountPerTransfer = 16
	reqBody, err := json.Marshal(exportOptions)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	req, err := http.NewRequest("POST", apiURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := DebugHTTP(client, req)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	defer resp.Body.Close()
	var task Task
	err = json.NewDecoder(resp.Body).Decode(&task)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	debugReturn(task.TaskTag, nil)
	return task.TaskTag, nil
}

func Import(newVMName, folder string) (string, error) {
	debugReturn := DebugCall(newVMName, folder)

	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   Config.Scale.Host,
		Path:   "/rest/v1/VirDomain/import",
	}
	var importOptions ImportOptions
	importOptions.Source.PathURI = (&url.URL{
		Scheme: "smb",
		User: url.UserPassword(
			smbUserAndDomain(),
			Config.SMB.Password,
		),
		Host: Config.SMB.Host,
		Path: path.Join("/", Config.SMB.ShareName, folder),
	}).String()
	importOptions.Source.Format = "qcow2"
	importOptions.Source.AllowNonSequentialWrites = true
	importOptions.Source.ParallelCountPerTransfer = 16
	importOptions.Template.Name = newVMName
	reqBody, err := json.Marshal(importOptions)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	req, err := http.NewRequest("POST", apiURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := DebugHTTP(client, req)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	defer resp.Body.Close()
	var task Task
	err = json.NewDecoder(resp.Body).Decode(&task)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	debugReturn(task.TaskTag, nil)
	return task.TaskTag, nil
}

func Upload(filename string, fileSize int64, file io.Reader) (string, error) {
	debugReturn := DebugCall(filename, fileSize, file)

	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   Config.Scale.Host,
		Path:   "/rest/v1/VirtualDisk/upload",
		RawQuery: url.Values{
			"filename": []string{filename},
			"filesize": []string{strconv.FormatInt(fileSize, 10)},
		}.Encode(),
	}
	req, err := http.NewRequest("PUT", apiURL.String(), file)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = fileSize
	resp, err := client.Do(req)
	if err != nil {
		debugReturn("", err)
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		debugReturn("", err)
		return "", err
	}

	var task Task
	err = json.Unmarshal(respBody, &task)
	if err != nil {
		err := fmt.Errorf("error unmarshalling task: %w\n%s", err, respBody)
		debugReturn("", err)
		return "", err
	}
	if task.CreatedUUID == "" {
		err := fmt.Errorf("task createdUUID is empty: %s", respBody)
		debugReturn("", err)
		return "", err
	}

	debugReturn(task.CreatedUUID, nil)
	return task.CreatedUUID, nil
}

func smbUserAndDomain() string {
	if Config.SMB.Domain == "" {
		return Config.SMB.Username
	} else {
		return Config.SMB.Domain + ";" + Config.SMB.Username
	}
}
