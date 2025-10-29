package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	ping "github.com/prometheus-community/pro-bing"
	"gopkg.in/ini.v1"
)

var (
	httpClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
)

type ConfigIni struct {
	ServerUrl   string
	ServerToken string
	ServerPort  string
	LogPath     string
}
type version struct {
	Major string `json:"major"`
	Minor string `json:"minor"`
	Patch string `json:"patch"`
	Git   string `json:"git"`
	Code  string `json:"code"`
	Date  string `json:"date"`
}

type VersionStruct struct {
	Hotel_id string `json:"hotel_id"`
	Status   bool   `json:"status"`
	Major    int    `json:"major"`
	Minor    int    `json:"minor"`
	Patch    string `json:"patch"`
	Git      string `json:"git"`
	Code     string `json:"code"`
	Date     string `json:"date"`
	CertName string `json:"hotel_name_certification"`
}

type HotelStruct struct {
	HotelRegion       string `json:"hotel_region"`
	HotelAdminID      string `json:"hotel_admin_id"`
	HotelCertName     string `json:"hotel_name_certification"`
	HotelVpnIPAddress string `json:"hotel_vpn_ip_address"`
	HotelVpnPort      string `json:"hotel_vpn_port"`
}

var confiServer ConfigIni
var pingerlog *log.Logger

func get_config() error {
	cfg, err := ini.Load("config.ini")
	if err != nil {
		return err
	}
	confiServer.ServerUrl = cfg.Section("server").Key("url").String()
	confiServer.ServerToken = cfg.Section("server").Key("token").String()
	confiServer.LogPath = cfg.Section("server").Key("logpath").String()

	if confiServer.ServerUrl == "" || confiServer.ServerToken == "" {
		return fmt.Errorf("missing fields in config.ini")
	}
	return nil
}

func get_data() error {
	token := fmt.Sprintf("Bearer %s", confiServer.ServerToken)
	url := fmt.Sprintf("%s/backup/gethotels", confiServer.ServerUrl)
	method := "GET"

	client := &http.Client{}
	req, err := http.NewRequest(method, url, nil)

	if err != nil {
		fmt.Println(err)
		return err
	}
	req.Header.Add("Authorization", token)

	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	var json_data []HotelStruct
	if err := json.Unmarshal(body, &json_data); err != nil {
		return fmt.Errorf("unmarshal error: %w, raw JSON: %s", err, string(body))
	}

	for _, v := range json_data {
		list := strings.Split(v.HotelVpnIPAddress, " ")
		for _, ip_value := range list {
			ip_value = strings.TrimSpace(ip_value)
			if ip_value == "" || ip_value == "0.0.0.0" {
				pingerlog.Println(v.HotelAdminID, "No IP")
				continue
			} else {
				ping_hosts(ip_value, v.HotelCertName, v.HotelAdminID)
			}
		}
	}

	return nil
}

func ping_hosts(ip_value, certname, hotel_id string) {
	if ip_value == "" || ip_value == "0.0.0.0" {
		return
	}
	pinger, err := ping.NewPinger(ip_value)
	if err != nil {
		pingerlog.Println("Error creating pinger for IP", ip_value, ":", err)
		return
	}
	defer pinger.Stop()
	pinger.SetPrivileged(true) // Не требует root
	pinger.Count = 2
	pinger.Timeout = 10 * time.Second

	if err := pinger.Run(); err != nil {
		pingerlog.Println("Error running pinger for IP", ip_value, ":", err)
		return
	}

	stats := pinger.Statistics()
	if stats.PacketsRecv > 0 {
		version_data, err := get_version(ip_value)
		if err != nil {
			pingerlog.Printf("Failed to get version for hotel_id:%s error: %v", hotel_id, err)
		}
		version_data.CertName = certname
		version_data.Hotel_id = hotel_id
		version_data.Status = true

		err = send_status(version_data)
		if err != nil {
			pingerlog.Printf("send_status hotel_id:%s error: %v", hotel_id, err)
		}
	} else {
		err := send_status(VersionStruct{Status: false, Hotel_id: hotel_id, CertName: certname})
		pingerlog.Printf("send_status hotel_id:%s error: %v", hotel_id, err)
	}
}

func get_version(ip string) (VersionStruct, error) {
	var ver version
	url := "http://" + ip + "/version.json"
	resp, err := http.Get(url)
	if err == nil && resp.StatusCode == http.StatusOK {

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return VersionStruct{}, err
		}
		err = json.Unmarshal(data, &ver)
		if err != nil {
			return VersionStruct{}, err
		}
		major, err := strconv.Atoi(ver.Major)
		if err != nil {
			return VersionStruct{}, err
		}
		minor, err := strconv.Atoi(ver.Minor)
		if err != nil {
			return VersionStruct{}, err
		}

		return VersionStruct{
			Major: major,
			Minor: minor,
			Patch: ver.Patch,
			Git:   ver.Git,
			Code:  ver.Code,
			Date:  ver.Date,
		}, nil
	}
	url = "http://" + ip + "/index.html"
	resp, err = http.Get(url)
	if err != nil {
		return VersionStruct{}, err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "var version") {
			data := strings.Fields(scanner.Text())
			version_data := strings.ReplaceAll(data[3], "'", "")
			version_data = strings.ReplaceAll(version_data, ";", "")
			version_parts := strings.Split(version_data, ".")
			if len(version_parts) >= 3 {
				major, err := strconv.Atoi(version_parts[0])
				if err != nil {
					return VersionStruct{}, err
				}
				minor, err := strconv.Atoi(version_parts[1])
				if err != nil {
					return VersionStruct{}, err
				}
				return VersionStruct{
					Major: major,
					Minor: minor,
					Patch: version_parts[2],
					Git:   "0", Code: "0", Date: "0"}, nil

			}
		}
	}
	return VersionStruct{}, fmt.Errorf("unable to determine version for IP: %s", ip)
}

func send_status(version VersionStruct) error {
	token := fmt.Sprintf("Bearer %s", confiServer.ServerToken)
	data, err := json.Marshal(version)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", confiServer.ServerUrl+"/dashboard/ping", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Add("Authorization", token)

	resp, err := httpClient.Do(req)
	if err != nil {
		pingerlog.Println(err)
		return err
	}
	defer resp.Body.Close()
	return nil
}

func main() {
	if err := get_config(); err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	pingerlog = log.New(os.Stdout, "pinger: ", log.LstdFlags)

	for {
		pingerlog.Println("*** Get data from server ***")
		get_data()
		pingerlog.Println("*** END ***")
		time.Sleep(120 * time.Second)
	}
}
