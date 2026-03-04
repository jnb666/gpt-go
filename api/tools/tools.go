package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

var Client = http.Client{Timeout: 30 * time.Second}

type Header struct {
	Key   string
	Value string
}

// HTTP get request for uri with optional headers. Unmarshals JSON response into reply.
func Get(uri string, reply any, headers ...Header) (http.Header, error) {
	log.Debugf("Tools GET %s", uri)
	req, _ := http.NewRequest("GET", uri, nil)
	for _, h := range headers {
		req.Header.Set(h.Key, h.Value)
	}
	return do(req, reply)
}

// HTTP post request for uri with JSON request and optional headers. Unmarshals JSON response into reply.
func Post(uri string, request, reply any, headers ...Header) (http.Header, error) {
	log.Debugf("Tools POST %s %+v", uri, request)
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("POST", uri, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, h := range headers {
		req.Header.Set(h.Key, h.Value)
	}
	return do(req, reply)
}

func do(req *http.Request, reply any) (http.Header, error) {
	resp, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.Header, fmt.Errorf("HTTP error: %s", resp.Status)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.Header, err
	}
	return resp.Header, json.Unmarshal(body, reply)
}
