package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	tofu "github.com/9072997/golang-tofu"
)

type VM struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	Tags        string `json:"tags"`
	IsTransient bool   `json:"isTransient"`
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
	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		return nil, err
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   Config.Scale.Host,
		Path:   "/rest/v1/VirDomain",
	}
	req, err := http.NewRequest("GET", apiURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var vms []VM
	err = json.NewDecoder(resp.Body).Decode(&vms)
	if err != nil {
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

	return vmMap, nil
}

func GetTask(taskTag string) (*Task, error) {
	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
		return nil, err
	}
	apiURL := url.URL{
		Scheme: "https",
		Host:   Config.Scale.Host,
		Path:   "/rest/v1/TaskTag/" + url.PathEscape(taskTag),
	}
	req, err := http.NewRequest("GET", apiURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tasks []Task
	err = json.NewDecoder(resp.Body).Decode(&tasks)
	if err != nil {
		return nil, err
	}

	if len(tasks) != 1 {
		return nil, fmt.Errorf("expected 1 task, got %d", len(tasks))
	}
	return &tasks[0], nil
}

func Export(vmUUID, folder string) (string, error) {
	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
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
		return "", err
	}
	req, err := http.NewRequest("POST", apiURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var task Task
	err = json.NewDecoder(resp.Body).Decode(&task)
	if err != nil {
		return "", err
	}
	return task.TaskTag, nil
}

func Import(newVMName, folder string) (string, error) {
	client, err := tofu.GetTofuClient(Config.Scale.CertFingerprint)
	if err != nil {
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
		return "", err
	}
	req, err := http.NewRequest("POST", apiURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(Config.Scale.Username, Config.Scale.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var task Task
	err = json.NewDecoder(resp.Body).Decode(&task)
	if err != nil {
		return "", err
	}
	return task.TaskTag, nil
}

func smbUserAndDomain() string {
	if Config.SMB.Domain == "" {
		return Config.SMB.Username
	} else {
		return Config.SMB.Domain + ";" + Config.SMB.Username
	}
}
