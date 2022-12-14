package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/ini.v1"
)

type ConfigIni struct {
	ServerUrl   string
	ServerToken string
	ServerPort  string
	LogPath     string
}

type VersionStruct struct {
	Hotel_id string `json:"hotel_id"`
	Status   string `json:"status"`
	Token    string `json:"token"`
	Major    string `json:"major"`
	Minor    string `json:"minor"`
	Patch    string `json:"patch"`
	Git      string `json:"git"`
	Code     string `json:"code"`
	Date     string `json:"date"`
}

type HotelStruct struct {
	Hotel_admin_id       string `json:"hotel_admin_id"`
	Hotel_name           string `json:"hotel_name"`
	Hotel_vpn_ip_address string `json:"hotel_vpn_ip_address"`
	Hotel_vpn_port       string `json:"hotel_vpn_port"`
}

// var wg sync.WaitGroup
var confiServer ConfigIni

var pingerlog *log.Logger

func get_config() {
	cfg, err := ini.Load("config.ini")
	if err != nil {
		pingerlog.Println(err)
		os.Exit(1)
	} else {
		confiServer.ServerUrl = cfg.Section("server").Key("url").String()
		confiServer.ServerPort = cfg.Section("server").Key("port").String()
		confiServer.ServerToken = cfg.Section("server").Key("token").String()
		confiServer.LogPath = cfg.Section("server").Key("logpath").String()
	}

}

func get_data() {
	var token string = confiServer.ServerToken
	request, err := http.PostForm("http://"+confiServer.ServerUrl+":"+confiServer.ServerPort+"/backup/gethotels/", url.Values{"token": {token}})
	if err != nil {
		pingerlog.Println(err)
	} else {
		defer request.Body.Close()
		data, err := ioutil.ReadAll(request.Body)
		if err != nil {
			pingerlog.Println(err)
		}
		json_data := make([]HotelStruct, 0)

		json.Unmarshal(data, &json_data)
		for _, v := range json_data {
			list := strings.Split(v.Hotel_vpn_ip_address, " ")
			for _, ip_value := range list {
				ip_value = strings.ReplaceAll(ip_value, " ", "")
				if ip_value == "" {
					pingerlog.Println(v.Hotel_admin_id, "No ip")
				} else {
					ping_hosts(ip_value, v.Hotel_admin_id)
				}
			}
		}
	}
}

func ping_hosts(ip_value string, hotel_id string) {
	out, err := exec.Command("fping", ip_value).Output()
	if err != nil {
		version_data, _ := get_version(hotel_id, ip_value)
		send_status(hotel_id, "0", version_data)
	} else {

		if strings.Contains(string(out), "alive") {
			version_data, status := get_version(hotel_id, ip_value)
			if status == false {
				send_status(hotel_id, "1", version_data)
			} else {
				send_status(hotel_id, "1", version_data)
			}
		}
		// wg.Done()
	}
}

func get_version(hotel_id string, ip string) (string, bool) {
	json_value, _ := json.Marshal(map[string]string{
		"major": "0",
		"minor": "0",
		"patch": "0",
		"git":   "0",
		"code":  "0",
		"date":  "0"})
	url := "http://" + ip + "/version.json"
	response, err := http.Get(url)
	if err != nil {
		return string(json_value), false
	} else {
		defer response.Body.Close()
		switch response.StatusCode {
		case 200:
			var listData VersionStruct
			data, err := ioutil.ReadAll(response.Body)
			if err != nil {
				return string(json_value), false
			} else {
				json.Unmarshal(data, &listData)

				json_value, _ := json.Marshal(map[string]string{
					"major": listData.Major,
					"minor": listData.Minor,
					"patch": listData.Patch,
					"git":   listData.Git,
					"code":  listData.Code,
					"date":  listData.Date,
				})
				return string(json_value), true
			}
		case 404:
			url := "http://" + ip + "/index.html"
			resp, err := http.Get(url)
			if err != nil {
				return string(json_value), false
			} else {

				defer resp.Body.Close()
				scanner := bufio.NewScanner(resp.Body)
				for scanner.Scan() {
					if strings.Contains(scanner.Text(), "var version") {
						data := strings.Fields(scanner.Text())
						version_data := data[3]
						version := strings.ReplaceAll(version_data, "'", "")
						version = strings.ReplaceAll(version, ";", "")

						version2 := strings.Split(version, ".")
						json_value, _ = json.Marshal(map[string]string{
							"major": version2[0],
							"minor": version2[1],
							"patch": version2[2],
							"git":   "0",
							"code":  "0",
							"date":  "0"})
						return string(json_value), true
					}
				}
			}
		default:
			return string(json_value), false
		}
	}
	return string(json_value), false
}

func send_status(hotel_id string, status string, version string) {
	var data_version VersionStruct
	data_version.Hotel_id = hotel_id
	data_version.Status = status
	data_version.Token = confiServer.ServerToken
	json.Unmarshal([]byte(version), &data_version)
	data, err := json.MarshalIndent(data_version, "", "  ")
	println(string(data))
	if err != nil {
		pingerlog.Println(err)
	} else {
		request, error := http.NewRequest("POST", "http://"+confiServer.ServerUrl+":"+confiServer.ServerPort+"/dashboard/goping/", bytes.NewBuffer([]byte(data)))
		if error != nil {
			pingerlog.Println(error.Error())
		}
		request.Header.Set("Content-Type", "application/json; charset=UTF-8")
		client := &http.Client{}
		response, error := client.Do(request)
		if error != nil {
			pingerlog.Println(error.Error())
		}
		pingerlog.Println("hotel_id:", hotel_id, "status response:", response.StatusCode)

	}
}

func main() {
	get_config()
	file, err := os.OpenFile(confiServer.LogPath+"pinger.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	pingerlog = log.New(file, "pinger: ", log.LstdFlags)
	pingerlog.Println("*** Get data from server ***")
	get_data()
	// wg.Wait()
	pingerlog.Println("*** END ***")
}
