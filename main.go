package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"spyrosoft-recruitment-task/base"
	"spyrosoft-recruitment-task/logger"
	"sync"
	"time"
)

const (
	ApiUrl        = "http://api.nbp.pl/api/exchangerates/rates/a/eur/last/100/"
	FetchInterval = 5
	FetchesAmount = 10
)

type IntervalHandler struct {
	wg     sync.WaitGroup
	waitCh chan int
}

func main() {
	logger.InitLogger()
	var mu sync.Mutex

	for {
		intervalHandler := &IntervalHandler{sync.WaitGroup{}, make(chan int)}

		intervalHandler.wg.Add(FetchesAmount)

		//locking mutex to avoid mixing logs from different goroutines
		mu.Lock()
		log.Println(" ======== BEGIN REQUESTS POOL ======== ")
		mu.Unlock()

		start := time.Now()
		for i := 0; i < FetchesAmount; i++ {
			go apiQueryWorker(i, &mu, &intervalHandler.wg)
		}

		go func() {
			// wait until all requests are processed
			intervalHandler.wg.Wait()

			//notify end of requests processing
			close(intervalHandler.waitCh)
		}()

		select {
		case <-intervalHandler.waitCh:
			// sleep until interval makes cycle
			elapsed := time.Since(start)
			time.Sleep(FetchInterval*time.Second - elapsed)
		case <-time.After(FetchInterval * time.Second):
			log.Println("Timeout, performing next requests group...")
		}

		mu.Lock()
		log.Println(" ======== END OF REQUESTS POOL ======== ")
		mu.Unlock()
	}

}

func apiQueryWorker(index int, mu *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()
	req, err := prepareHttpRequest()
	if err != nil {
		log.Fatalf("Failed to prepare GET request: %s", err)
		return
	}

	client := &http.Client{}

	startTime := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to perform GET request: %s", err)
		return
	}

	elapsed := time.Since(startTime)

	defer func() {
		err := resp.Body.Close()
		if err != nil {
			log.Fatalf("Failed to close response body: %s", err)
			return
		}
	}()

	statusCode := resp.StatusCode
	contentType := resp.Header.Get("Content-Type")

	// read gzip byte stream and decompress it into readable JSON
	content, err := decompressGzippedResponse(resp)
	if err != nil {
		log.Fatalf("Failed to read compressed body content: %s", err)
		return
	}

	isJsonValid := json.Valid(content)

	var summary base.ExchangeRatesSummary

	err = json.Unmarshal(content, &summary)
	if err != nil {
		log.Fatalf("Failed to unmarshall request content: %s", err)
		return
	}

	var rateOutOfScope []string

	for _, item := range summary.Rates {
		if item.Mid < 4.5 || item.Mid > 4.7 {
			day, month, year := item.EffectiveDate.Day(), item.EffectiveDate.Month(), item.EffectiveDate.Year()
			date := fmt.Sprintf("%d/%d/%d", day, month, year)
			rateOutOfScope = append(rateOutOfScope, date)
		}
	}

	//locking mutex to avoid mixing logs from different goroutines
	mu.Lock()
	logger.PrintReqInfo(index, elapsed, statusCode, contentType, isJsonValid, rateOutOfScope)
	mu.Unlock()
}

func prepareHttpRequest() (*http.Request, error) {
	req, err := http.NewRequest("GET", ApiUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare HTTP GET request: %s", err)
	}

	addHeaders(req)

	return req, nil
}

func addHeaders(req *http.Request) {
	req.Header.Set("Host", "api.nbp.pl")
	req.Header.Set("User-Agent", "Golang Program")
	req.Header.Set("Accept-Language", "pl-PL,pl;q=0.9,en-US;q=0.8,en;q=0.7")

	//gzip encoding results in a much smaller response body
	req.Header.Set("Accept-Encoding", "deflate, gzip")
}

func decompressGzippedResponse(response *http.Response) ([]byte, error) {
	gzipBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read compressed body content: %s", err)
	}

	bytesReader := bytes.NewReader(gzipBytes)
	gzipReader, err := gzip.NewReader(bytesReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %s", err)
	}

	content, err := ioutil.ReadAll(gzipReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read compressed body content: %s", err)
	}

	return content, nil
}
