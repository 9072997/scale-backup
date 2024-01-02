package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
)

var logFileMutex sync.Mutex
var logFileTruncated bool

func writeToLogFile(logEntry string) {
	if Config.Debug.LogFile == "" {
		return
	}

	// lock log file
	logFileMutex.Lock()
	defer logFileMutex.Unlock()

	// truncate log file if necessary
	if !logFileTruncated {
		err := os.Truncate(Config.Debug.LogFile, 0)
		if err != nil {
			panic(err)
		}
		logFileTruncated = true
	}

	// open file for appending
	f, err := os.OpenFile(Config.Debug.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// write log line
	_, err = f.WriteString(logEntry)
	if err != nil {
		panic(err)
	}
}

func DebugCall(args ...any) func(...any) {
	if Config.Debug.LogFile == "" {
		return func(...any) {}
	}

	// get name of calling function
	pc, _, _, _ := runtime.Caller(1)
	caller := runtime.FuncForPC(pc).Name()

	// format arguments
	var jsonArgs []string
	for _, arg := range args {
		jsonArg, err := json.Marshal(arg)
		if err != nil {
			jsonArg = []byte(fmt.Sprintf("%#v", arg))
		}
		jsonArgs = append(jsonArgs, string(jsonArg))
	}

	// format log line
	argsStr := strings.Join(jsonArgs, ", ")
	logLine := fmt.Sprintf("%s(%s)\n", caller, argsStr)

	// write log line
	writeToLogFile(logLine)

	// return a function that can be called to log the return value
	return func(ret ...any) {
		// get line number of calling function
		_, _, line, _ := runtime.Caller(1)

		var jsonRet []string
		for _, r := range ret {
			jsonR, err := json.Marshal(r)
			if err != nil {
				jsonR = []byte(fmt.Sprintf("%#v", r))
			}
			jsonRet = append(jsonRet, string(jsonR))
		}
		retStr := strings.Join(jsonRet, ", ")
		origLine := strings.TrimRight(logLine, "\n")
		logLine = fmt.Sprintf("%s:%d = %s\n", origLine, line, retStr)
		writeToLogFile(logLine)
	}
}

func DebugHTTP(c *http.Client, r *http.Request) (*http.Response, error) {
	if Config.Debug.LogFile == "" {
		return c.Do(r)
	}

	var logEntry strings.Builder
	logEntry.WriteString(fmt.Sprintf(
		"=== %s %s ===\n",
		r.Method,
		r.URL.RequestURI(),
	))

	// get request body
	var reqBody []byte
	if r.Body != nil {
		reqBody, _ = io.ReadAll(r.Body)
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(reqBody))
	}
	if Config.Debug.RedactPasswords {
		reqBody = bytes.Replace(
			reqBody,
			[]byte(Config.SMB.Password),
			[]byte("REDACTED"),
			1,
		)
	}
	if len(reqBody) > 0 {
		logEntry.WriteString(fmt.Sprintf(
			"Request body:\n%s\n",
			reqBody,
		))
	}

	// do request
	resp, reqErr := c.Do(r)

	// get response body
	var respBody []byte
	if resp != nil && resp.Body != nil {
		respBody, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
	}
	if len(respBody) > 0 {
		logEntry.WriteString(fmt.Sprintf(
			"Response body:\n%s\n",
			respBody,
		))
	}

	// write log line
	logEntry.WriteString(logEntry.String())

	return resp, reqErr
}
