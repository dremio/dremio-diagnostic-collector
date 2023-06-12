//	Copyright 2023 Dremio Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package restclient

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

var client *http.Client

func InitClient(allowInsecureSSL bool) {
	tr := &http.Transport{
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ExpectContinueTimeout: 30 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: allowInsecureSSL},
	}
	client = &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}
}

func APIRequest(url string, pat string, request string, headers map[string]string) ([]byte, error) {
	simplelog.Debugf("Requesting %s", url)
	req, err := http.NewRequest(request, url, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create request due to error %v", err)
	}
	authorization := "Bearer " + pat
	req.Header.Set("Authorization", authorization)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf(res.Status)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func PostQuery(url string, pat string, headers map[string]string, systable string) (string, error) {
	simplelog.Debugf("Collecting sys." + systable)
	sqlbody := "{\"sql\": \"SELECT * FROM sys." + systable + "\"}"

	req, err := http.NewRequest("POST", url, strings.NewReader(sqlbody))
	if err != nil {
		return "", fmt.Errorf("unable to create request due to error %v", err)
	}
	authorization := "Bearer " + pat
	req.Header.Set("Authorization", authorization)

	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := client.Do(req)

	if err != nil {
		return "", err
	}
	if res.StatusCode != 200 {
		return "", fmt.Errorf(res.Status)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	var job map[string]string
	if err := json.Unmarshal(body, &job); err != nil {
		return "", err
	}
	return job["id"], nil
}
