// 연관검색어 크롤링
// GOOS=windows GOARCH=amd64 go build -o output.exe
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/tealeg/xlsx/v3"
)

type naverRes struct {
	Query  []string     `json:"query"`
	Answer []string     `json:"answer"`
	Intend []string     `json:"intend"`
	Items  [][][]string `json:"items"`
}

type daumRes struct {
	Q       string      `json:"q"`
	Tltm    interface{} `json:"tltm"`
	Subkeys []struct {
		Keyword     string        `json:"keyword"`
		Highlighted [][]int       `json:"highlighted"`
		MetaCnt     int           `json:"metaCnt"`
		Meta        []interface{} `json:"meta"`
	} `json:"subkeys"`
}

type Result struct {
	Naver []string `json:"naver"`
	Daum  []string `json:"daum"`
}

func getLineInfo(skip int) (string, int) {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "?", 0
	}
	return file, line
}

func getCurrentTimestampMillis() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String(), nil
			}
		}
	}
	return "", fmt.Errorf("no IP address found")
}

// 로그 파일에 에러를 기록하는 함수
func logError(err error, skip int) {
	ip, ipErr := getLocalIP()
	if ipErr != nil {
		ip = "unknown"
	}

	// 로그 파일 열기 (추가 모드)
	logFile, logErr := os.OpenFile("error.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if logErr != nil {
		fmt.Println("로그 파일을 열 수 없습니다:", logErr)
		return
	}
	defer logFile.Close()

	// 현재 파일명과 라인 번호 가져오기
	file, line := getLineInfo(skip + 1) // logError 호출 위치를 정확히 잡기 위해 skip + 1 사용

	// 현재 시간과 에러 메시지 기록
	logWriter := bufio.NewWriter(logFile)
	logEntry := fmt.Sprintf("%s: %s (File: %s, Line: %d, IP: %s)\n", time.Now().Format(time.RFC3339), err.Error(), file, line, ip)
	if _, logErr := logWriter.WriteString(logEntry); logErr != nil {
		fmt.Println("로그 파일에 쓸 수 없습니다:", logErr)
	}
	logWriter.Flush()
}

func logApp(totalCount int) {
	ip, ipErr := getLocalIP()
	if ipErr != nil {
		ip = "unknown"
	}

	// 로그 작성
	logEntry := fmt.Sprintf("%s: 총 데이터 개수: %d, IP: %s\n", time.Now().Format(time.RFC3339), totalCount, ip)
	logFile, logErr := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if logErr != nil {
		fmt.Println("로그 파일을 열 수 없습니다:", logErr)
		return
	}
	defer logFile.Close()

	if _, logErr = logFile.WriteString(logEntry); logErr != nil {
		fmt.Println("로그 파일에 쓸 수 없습니다:", logErr)
	}
}

func GET(url string, headers map[string]string) (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	for key, value := range headers {
		req.Header.Add(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func decodeJSONP(jsonp string, mode string) (interface{}, error) {
	// JSONP 함수 호출 제거
	start := strings.Index(jsonp, "(")
	end := strings.LastIndex(jsonp, ")")
	if start == -1 || end == -1 || start >= end {
		file, line := getLineInfo(2)
		return nil, fmt.Errorf("invalid JSONP format (File: %s, Line: %d)", file, line)
	}

	jsonStr := jsonp[start+1 : end]
	switch mode {
	case "naver":
		var response naverRes
		err := json.Unmarshal([]byte(jsonStr), &response)
		if err != nil {
			file, line := getLineInfo(2)
			return nil, fmt.Errorf("error unmarshalling JSONP (File: %s, Line: %d): %v", file, line, err)
		}
		return &response, nil
	case "daum":
		var response daumRes
		err := json.Unmarshal([]byte(jsonStr), &response)
		if err != nil {
			file, line := getLineInfo(2)
			return nil, fmt.Errorf("error unmarshalling JSONP (File: %s, Line: %d): %v", file, line, err)
		}
		return &response, nil
	default:
		file, line := getLineInfo(2)
		return nil, fmt.Errorf("invalid mode (File: %s, Line: %d)", file, line)
	}
}

func POST(url string, data []byte, headers map[string]string) (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}

	for key, value := range headers {
		req.Header.Add(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// getKeyword 함수 정의
func getKeyword(filename string) ([]string, error) {
	// 파일 열기
	file, err := os.Open(filename)
	if err != nil {
		logError(err, 1)
		return nil, err
	}
	defer file.Close()

	// 스캐너를 사용하여 파일을 한 줄씩 읽기
	scanner := bufio.NewScanner(file)
	var keywords []string

	for scanner.Scan() {
		keywords = append(keywords, scanner.Text())
	}

	// 스캐너에서 에러가 발생했는지 확인
	if err := scanner.Err(); err != nil {
		logError(err, 1)
		return nil, err
	}

	return keywords, nil
}

func naver(keyword string) (*naverRes, error) {
	url := "https://ac.search.naver.com/nx/ac?q=" + keyword + "&con=1&frm=nv&ans=2&r_format=json&r_enc=UTF-8&r_unicode=0&t_koreng=1&run=2&rev=4&q_enc=UTF-8&st=100&_callback=_jsonp_4"
	headers := map[string]string{
		"Content-Type": "application/javascript; charset=UTF-8",
		"Referer":      "https://www.naver.com/",
		"User-Agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.5735.110 Safari/537.36",
	}

	res, err := GET(url, headers)
	if err != nil {
		return nil, err
	}

	data, err := decodeJSONP(res, "naver")
	if err != nil {
		return nil, err
	}

	if naverData, ok := data.(*naverRes); ok {
		return naverData, nil
	}
	return nil, fmt.Errorf("invalid data format")
}

func daum(keyword string) (*daumRes, error) {
	timestamp := getCurrentTimestampMillis()
	url := fmt.Sprintf("https://vmsuggest.search.daum.net/v2/sushi/pc/get?q=%s&callback=jsonp%d", keyword, timestamp)
	headers := map[string]string{
		"Content-Type": "application/javascript; charset=UTF-8",
		"Referer":      "https://www.daum.net/",
		"User-Agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.5735.110 Safari/537.36",
	}

	res, err := GET(url, headers)
	if err != nil {
		return nil, err
	}

	data, err := decodeJSONP(res, "daum")
	if err != nil {
		return nil, err
	}

	if daumData, ok := data.(*daumRes); ok {
		return daumData, nil
	}
	return nil, fmt.Errorf("invalid data format")
}

func aggregateData(naverData []*naverRes, daumData []*daumRes) Result {
	dataMap := make(map[string]struct{})
	var result Result

	for _, nRes := range naverData {
		for _, items := range nRes.Items {
			for _, item := range items {
				if len(item) > 0 {
					dataMap[item[0]] = struct{}{}
				}
			}
		}
	}

	for _, dRes := range daumData {
		for _, subkey := range dRes.Subkeys {
			dataMap[subkey.Keyword] = struct{}{}
		}
	}

	for key := range dataMap {
		if strings.Contains(key, "naver") {
			result.Naver = append(result.Naver, key)
		} else if strings.Contains(key, "daum") {
			result.Daum = append(result.Daum, key)
		} else {
			result.Naver = append(result.Naver, key)
			result.Daum = append(result.Daum, key)
		}
	}
	return result
}

func saveToExcel(result Result) error {
	// 현재 타임스탬프를 사용하여 파일명 생성
	timestamp := getCurrentTimestampMillis()
	fileName := fmt.Sprintf("result_%d.xlsx", timestamp)

	// 새로운 엑셀 파일 생성
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("Sheet1")
	if err != nil {
		return fmt.Errorf("failed to create sheet: %v", err)
	}

	// 네이버 키워드 작성
	for _, keyword := range result.Naver {
		row := sheet.AddRow()
		cellType := row.AddCell()
		cellType.Value = "네이버"
		cellKeyword := row.AddCell()
		cellKeyword.Value = keyword
	}

	// 다음 키워드 작성
	for _, keyword := range result.Daum {
		row := sheet.AddRow()
		cellType := row.AddCell()
		cellType.Value = "다음"
		cellKeyword := row.AddCell()
		cellKeyword.Value = keyword
	}

	// 엑셀 파일 저장
	err = file.Save(fileName)
	if err != nil {
		return fmt.Errorf("failed to save file: %v", err)
	}

	// 로그 작성
	totalCount := len(result.Naver) + len(result.Daum)
	logApp(totalCount)

	return nil
}

func main() {
	// getKeyword 함수 호출
	keywords, err := getKeyword("input.txt")
	if err != nil {
		logError(err, 1)
		return
	}

	var naverData []*naverRes
	var daumData []*daumRes

	// 결과 출력
	for _, keyword := range keywords {
		nData, err := naver(keyword)
		if err != nil {
			logError(err, 1)
			continue
		}
		naverData = append(naverData, nData)

		dData, err := daum(keyword)
		if err != nil {
			logError(err, 1)
			continue
		}
		daumData = append(daumData, dData)
	}

	// 데이터를 정리
	result := aggregateData(naverData, daumData)

	// 정리된 결과를 출력
	/*
		resultJSON, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			logError(err, 1)
			return
		}

		fmt.Println(string(resultJSON))
	*/

	// 엑셀 파일로 저장
	err = saveToExcel(result)
	if err != nil {
		logError(err, 1)
	} else {
		fmt.Println("결과 분류가 완료되었습니다. 결과파일을 확인하세요.")
		for {

		}
	}
}
