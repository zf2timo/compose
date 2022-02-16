/*
   Copyright 2022 Docker Compose CLI authors

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package compose

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Masterminds/semver"
	"github.com/spf13/cobra"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"
)

type selfUpdateOptions struct {
	unstable bool
	quiet    bool
}

type VersionResponse struct {
	Id      int    `json:"id"`
	Url     string `json:"url"`
	Name    string `json:"name,omitempty"`
	TagName string `json:"tag_name" json:"tag_name,omitempty"`
}

type AssetsResponse struct {
	Id   int    `json:"id"`
	Url  string `json:"url"`
	Name string `json:"name,omitempty"`
}

type AssetFileResponse struct {
	Size int    `json:"size"`
	Url  string `json:"browser_download_url"`
}

func selfUpdateCommand() *cobra.Command {
	opts := selfUpdateOptions{}
	cmd := &cobra.Command{
		Use:   "selfupdate",
		Short: "Install latest version of docker-compose",
		Args:  cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			runSelfUpdate(opts)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.unstable, "unstable", false, "Installs development version of docker-compose")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "Install updates without information messages")

	return cmd
}

func runSelfUpdate(opts selfUpdateOptions) {
	fmt.Println("Checking for new docker-compose version ...")

	url := "https://api.github.com/repos/docker/compose/releases"

	body, err := requestJson(url)

	var versions []VersionResponse
	jsonErr := json.Unmarshal(body, &versions)
	if jsonErr != nil {
		log.Fatal(jsonErr)
	}

	version := "2.2.2"
	//version := internal.Version
	currentVersion, err := semver.NewVersion(version)
	if err != nil {
		log.Fatal("Failed to parse current Version", err)
	}

	fmt.Println("Latest tag is ", versions[0].Name)

	nextVersion, err := semver.NewVersion(versions[0].TagName)
	if err != nil {
		log.Fatal("Failed to parse version from github releases", err)
	}

	if nextVersion.LessThan(currentVersion) {
		fmt.Println("Latest version is already installed")
		return
	}

	assetSuffix, err := matchArchitecture()
	if err != nil {
		log.Fatal(err)
		return
	}

	currentBinaryPath, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
		return
	}

	binaryFileUrl := fmt.Sprint("https://github.com/docker/compose/releases/download/v", nextVersion, "/docker-compose-", assetSuffix)
	binaryFilePath, err := ioutil.TempFile(currentBinaryPath, "docker-compose-")
	if err != nil {
		log.Fatal(err)
	}

	err = downloadFile(binaryFileUrl, binaryFilePath)
	if err != nil {
		log.Fatal(err)
	}

	binarySHAFile, err := ioutil.TempFile(os.TempDir(), "docker-compose-*.sha256")
	if err != nil {
		log.Fatal(err)
	}

	err = downloadFile(binaryFileUrl+".sha256", binarySHAFile)
	if err != nil {
		log.Fatal(err)
	}

	binaryFileContent, err := os.ReadFile(binaryFilePath.Name())
	if err != nil {
		log.Fatal(err)
	}

	computedSum := sha256.Sum256(binaryFileContent)
	downloadedSum, err := os.ReadFile(binarySHAFile.Name())
	if err != nil {
		log.Fatal(err)
	}

	if hex.EncodeToString(computedSum[:]) != string(downloadedSum[:64]) {
		fmt.Println("Sha256 hashes are not identically")
	}

	fmt.Println("Sha256 hashes identically. Replace current binary with new update")

	err = os.Rename(currentBinaryPath, currentBinaryPath+"_old")
	if err != nil {
		log.Fatal(err)
		return
	}

	fmt.Printf("Move downdloaded file from %s to %s\n", binaryFilePath.Name(), currentBinaryPath+"_tmp")
	err = os.Rename(binaryFilePath.Name(), currentBinaryPath+"_tmp")
	if err != nil {
		log.Fatal(err)
		return
	}
}

func searchAssetUrl(err error, assets []AssetsResponse, s string) string {
	assetIndex, err := searchAssets(assets, s)
	assertUrl := assets[assetIndex].Url
	if err != nil {
		log.Fatal(err)
	}
	return assertUrl
}

func downloadFile(url string, file *os.File) error {

	resp, err := http.Get(url)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func requestJson(url string) ([]byte, error) {
	client := http.Client{
		Timeout: 30 * time.Second,
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github.v4+json")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if res.Body != nil {
		defer res.Body.Close()
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func matchArchitecture() (string, error) {

	if runtime.GOOS == "darwin" {
		if runtime.GOARCH == "arm64" {
			return "darwin-aarch64", nil
		} else if runtime.GOARCH == "x86_64" {
			return "darwin-x86_64", nil
		}
	} else if runtime.GOOS == "linux" {
		if runtime.GOARCH == "s390x" {
			return "linux-s390x", nil
		} else if runtime.GOARCH == "arm64" {
			return "linux-aarch64", nil
		} else if runtime.GOARCH == "" {
			return "linux-armv6", nil
		} else if runtime.GOARCH == "" {
			return "linux-armv7", nil
		} else if runtime.GOARCH == "amd64" {
			return "linux-x86_64", nil
		}
	} else if runtime.GOOS == "windows" && runtime.GOARCH == "x86_64" {
		return "windows-x86_64", nil
	}

	return "", fmt.Errorf("no matching assets was found for %q and %q", runtime.GOOS, runtime.GOARCH)
}

func searchAssets(assets []AssetsResponse, needle string) (int, error) {
	for i := range assets {
		if assets[i].Name == needle {
			return i, nil
		}
	}

	return 0, fmt.Errorf("No matching asset was found")
}
