package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"
)

// newHTTPClient returns a new HTTP client that is configured according to the
// proxy and TLS settings in the associated connection configuration.
func newHTTPClient() (*http.Client, error) {
	// Configure proxy if needed.
	var dial func(network, addr string) (net.Conn, error)

	// Configure TLS if needed.
	var tlsConfig *tls.Config
	tlsConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	// Create and return the new HTTP client potentially configured with a
	// proxy and TLS.
	client := http.Client{
		Transport: &http.Transport{
			Dial:            dial,
			TLSClientConfig: tlsConfig,
			DialContext: (&net.Dialer{
				Timeout:   time.Duration(3) * time.Second,
				KeepAlive: time.Duration(1) * time.Second,
				DualStack: true,
			}).DialContext,
		},
	}
	return &client, nil
}

func RpcResult(url, authstr, method string, params []interface{}, id string) []byte {

	paramStr, err := json.Marshal(params)
	if err != nil {
		return nil
	}
	jsonStr := []byte(`{"jsonrpc": "2.0", "method": "` + method +
		`", "params": ` + string(paramStr) + `, "id": "` + id + `"}`)
	bodyBuff := bytes.NewBuffer(jsonStr)
	httpRequest, err := http.NewRequest("POST", url, bodyBuff)
	if err != nil {
		return nil
	}
	httpRequest.Close = true
	httpRequest.Header.Set("Content-Type", "application/json")
	// Configure basic access authorization.
	// 使用 Base64 编码用户名和密码
	if authstr != "" {
		auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(authstr))

		// 将授权信息添加到请求头中
		httpRequest.Header.Add("Authorization", auth)
	}

	// Create the new HTTP client that is configured according to the user-
	// specified options and submit the request.
	httpClient, err := newHTTPClient()
	if err != nil {
		return nil
	}
	defer httpClient.CloseIdleConnections()
	httpClient.Timeout = time.Duration(3) * time.Second
	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil
	}
	body, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return nil
	}

	if httpResponse.StatusCode != 200 {
		_ = httpResponse.Body.Close()
		time.Sleep(30 * time.Second)
		return nil
	}
	_ = httpResponse.Body.Close()
	return body
}
