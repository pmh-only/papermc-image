package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

var PROJECT_NAME string
var VERSION_NAME string
var BUILD_ID string
var DOWNLOAD_NAME string
var DOWNLOAD_URL string

const userAgent = "papermc-image/1.0 (https://github.com/pmh-only/papermc-image)"

var apiClient = &http.Client{Timeout: 30 * time.Second}

type buildResponse struct {
	ID        int    `json:"id"`
	Channel   string `json:"channel"`
	Downloads map[string]struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"downloads"`
}

var GITHUB_OUTPUT string
var DISABLE_VERSION_UPDATE_CHECK bool

func init() {
	project_name, isProjectNameProvided := os.LookupEnv("PROJECT_NAME")
	PROJECT_NAME = project_name

	log.Printf("PROJECT_NAME: %s\n", PROJECT_NAME)

	if !isProjectNameProvided {
		log.Fatalln("Required environment variable, PROJECT_NAME does not provided.")
	}

	github_output, isGithubOutputProvided := os.LookupEnv("GITHUB_OUTPUT")
	GITHUB_OUTPUT = github_output
	DISABLE_VERSION_UPDATE_CHECK = !isGithubOutputProvided

	if DISABLE_VERSION_UPDATE_CHECK {
		log.Println("Version update check disabled")
	}
}

func main() {
	log.Println("Fetching VERSION_NAME list...")

	apiURL := fmt.Sprintf("https://fill.papermc.io/v3/projects/%s", url.PathEscape(PROJECT_NAME))
	var project struct {
		Versions json.RawMessage `json:"versions"`
	}
	if err := fetchJSON(apiURL, &project); err != nil {
		log.Fatal(err)
	}
	var err error
	VERSION_NAME, err = latestVersion(project.Versions)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Fetched VERSION_NAME: %s\n", VERSION_NAME)
	log.Println("Fetching BUILD_ID list...")

	var build buildResponse
	if err := fetchJSON(fmt.Sprintf("%s/versions/%s/builds/latest", apiURL, url.PathEscape(VERSION_NAME)), &build); err != nil {
		log.Fatal(err)
	}
	BUILD_ID = fmt.Sprint(build.ID)
	log.Printf("Fetched BUILD_ID: %s\n", BUILD_ID)
	log.Println("Fetching DOWNLOAD_NAME list...")

	is_experimental_build := "false"

	if build.Channel != "STABLE" && build.Channel != "RECOMMENDED" {
		log.Println("Experimental build detected")
		is_experimental_build = "true"
	}

	download, ok := build.Downloads["server:default"]
	if !ok || download.Name == "" || download.URL == "" {
		log.Fatalln("PaperMC API did not provide a server:default download")
	}
	DOWNLOAD_NAME = download.Name
	DOWNLOAD_URL = download.URL
	log.Printf("Fetched DOWNLOAD_NAME: %s\n", DOWNLOAD_NAME)

	if DISABLE_VERSION_UPDATE_CHECK {
		return
	}

	log.Println("Checking version history...")

	body, err := os.ReadFile("previous_args.json")
	if err != nil {
		log.Fatalf("Version history file, previous_args.json is not readable: %s\n", err.Error())
	}

	log.Printf("Previous args: %s", body)
	log.Println("Calculating NEEDS_UPDATE")

	var file map[string]interface{}
	if err := json.Unmarshal(body, &file); err != nil {
		log.Fatalln("Version history file, previous_args.json is corrupted")
	}

	needs_update := false

	previous_data := file[PROJECT_NAME]
	if previous_data == nil {
		needs_update = true
	}

	if previous_data != nil {
		previous_data := previous_data.(map[string]interface{})

		previous_VERSION_NAME := previous_data["VERSION_NAME"].(string)
		if VERSION_NAME != previous_VERSION_NAME {
			needs_update = true
		}

		previous_BUILD_ID := previous_data["BUILD_ID"].(string)
		if BUILD_ID != previous_BUILD_ID {
			needs_update = true
		}

		previous_DOWNLOAD_NAME := previous_data["DOWNLOAD_NAME"].(string)
		if DOWNLOAD_NAME != previous_DOWNLOAD_NAME {
			needs_update = true
		}
	}

	if !needs_update {
		os.WriteFile(GITHUB_OUTPUT, []byte("NEEDS_UPDATE=false"), 0666)
		log.Println("NEEDS_UPDATE: false")
		return
	}

	output_body := fmt.Sprintf(
		"NEEDS_UPDATE=true\nVERSION_NAME=%s\nBUILD_ID=%s\nDOWNLOAD_NAME=%s\nDOWNLOAD_URL=%s\nIS_EXPERIMENTAL_BUILD=%s\n",
		VERSION_NAME, BUILD_ID, DOWNLOAD_NAME, DOWNLOAD_URL, is_experimental_build)

	os.WriteFile(GITHUB_OUTPUT, []byte(output_body), 0666)
	log.Println("NEEDS_UPDATE: true")

	new_data := file
	new_data[PROJECT_NAME] = map[string]string{}
	new_data[PROJECT_NAME].(map[string]string)["VERSION_NAME"] = VERSION_NAME
	new_data[PROJECT_NAME].(map[string]string)["BUILD_ID"] = BUILD_ID
	new_data[PROJECT_NAME].(map[string]string)["DOWNLOAD_NAME"] = DOWNLOAD_NAME

	new_body, _ := json.Marshal(new_data)

	os.WriteFile("./previous_args.json", new_body, 0666)
}

// Version groups and their entries are returned newest first. Preserve their
// JSON order rather than decoding groups into an unordered Go map.
func latestVersion(versions json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(versions))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return "", fmt.Errorf("PaperMC API returned invalid version groups")
	}
	for decoder.More() {
		if _, err := decoder.Token(); err != nil {
			return "", err
		}
		var group []string
		if err := decoder.Decode(&group); err != nil {
			return "", err
		}
		if len(group) > 0 && group[0] != "" {
			return group[0], nil
		}
	}
	return "", fmt.Errorf("PaperMC API returned no versions")
}

func fetchJSON(apiURL string, data interface{}) error {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := apiClient.Do(req)
	if err != nil {
		return fmt.Errorf("PaperMC API call failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("PaperMC API returned HTTP %d: %s", resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(data); err != nil {
		return fmt.Errorf("PaperMC API returned invalid JSON: %w", err)
	}
	return nil
}
