package client

import (
	"bufio"
	"bytes"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
)

func DoBatchRequest(endpoint string, requests map[string]*http.Request) map[string]*http.Response {

	buffer := new(bytes.Buffer)
	mw := multipart.NewWriter(buffer)
	err := mw.SetBoundary("batch_boundary")
	if err != nil {
		panic(err)
	}

	for key, req := range requests {
		pw, err := mw.CreatePart(map[string][]string{
			"Content-Type": {"application/http"},
			"Content-ID":   {key},
		})
		if err != nil {
			panic(err)
		}

		req.Header.Set("User-Agent", "")
		req.Write(pw)
	}

	mw.Close()

	batchReq, err := http.NewRequest("POST", endpoint, buffer)
	if err != nil {
		panic(err)
	}
	batchReq.Header.Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())

	// dump the batch request for debugging
	dump, err := httputil.DumpRequestOut(batchReq, true)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(dump))

	// Send the batch request
	client := &http.Client{}
	batchResp, err := client.Do(batchReq)
	if err != nil {
		panic(err)
	}
	defer batchResp.Body.Close()

	fmt.Println("Response Status:", batchResp.Status)
	fmt.Println("Response Headers:", batchResp.Header)

	mediaType, params, err := mime.ParseMediaType(batchResp.Header.Get("Content-Type"))
	if err != nil {
		panic(err)
	}

	if mediaType != "multipart/mixed" {
		panic("Expected multipart/mixed response")
	}

	responses := make(map[string]*http.Response)

	mr := multipart.NewReader(batchResp.Body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}

		fmt.Println("Part Headers:", part.Header)

		contentID := part.Header.Get("Content-ID")

		req, ok := requests[contentID]
		if !ok {
			fmt.Println("Unknown Content-ID:", contentID)
			continue
		}

		reader := bufio.NewReader(part)
		resp, err := http.ReadResponse(reader, req)
		if err != nil {
			panic(err)
		}

		responses[contentID] = resp
	}

	return responses
}
