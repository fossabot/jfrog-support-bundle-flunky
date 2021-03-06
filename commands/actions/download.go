package actions

import (
	"context"
	"errors"
	"fmt"
	"github.com/jfrog/jfrog-client-go/utils/io/fileutils"
	"github.com/jfrog/jfrog-client-go/utils/log"
	flunkyhttp "github.com/jfrog/jfrog-support-bundle-flunky/commands/http"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type downloadSupportBundleHTTPClient interface {
	GetURL() string
	DownloadSupportBundle(bundleID string) (*http.Response, error)
	GetSupportBundleStatus(bundleID string) (int, []byte, error)
}

// DownloadSupportBundle downloads a Support Bundle.
func DownloadSupportBundle(ctx context.Context, client downloadSupportBundleHTTPClient, timeout time.Duration,
	retryInterval time.Duration, bundleID BundleID) (string, error) {
	log.Debug(fmt.Sprintf("Download Support Bundle %s from %s", bundleID, client.GetURL()))

	err := waitUntilSupportBundleIsReady(ctx, client, retryInterval, timeout, bundleID)
	if err != nil {
		return "", err
	}

	dirPath, err := fileutils.CreateTempDir()
	if err != nil {
		return "", err
	}
	tmpFilePath := filepath.Join(dirPath, fmt.Sprintf("%s.zip", bundleID))
	tmpZipFile, err := os.Create(tmpFilePath)
	if err != nil {
		return "", err
	}
	defer handleClose(tmpZipFile)

	err = downloadSupportBundleAndWriteToFile(client, tmpZipFile, bundleID)
	if err != nil {
		return "", err
	}

	log.Debug(fmt.Sprintf("Downloaded Support Bundle to %s", tmpFilePath))
	return tmpFilePath, nil
}

func downloadSupportBundleAndWriteToFile(client downloadSupportBundleHTTPClient, tmpZipFile *os.File, bundleID BundleID) error {
	resp, err := client.DownloadSupportBundle(string(bundleID))
	if err != nil {
		return err
	}
	defer handleClose(resp.Body)
	log.Debug(fmt.Sprintf("Got %d", resp.StatusCode))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http request failed with: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	_, err = io.Copy(tmpZipFile, resp.Body)
	if err != nil {
		return err
	}
	return nil
}

func waitUntilSupportBundleIsReady(ctx context.Context, client downloadSupportBundleHTTPClient,
	retryInterval time.Duration, timeout time.Duration, bundleID BundleID) error {
	ctxWithTimeout, cancelCtx := context.WithTimeout(ctx, timeout)
	defer cancelCtx()
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctxWithTimeout.Done():
			return errors.New("timeout waiting for support bundle to be ready")
		case <-ticker.C:
			sbStatus, err := getBundleStatus(bundleID, client)
			if err != nil {
				return err
			}

			log.Debug(fmt.Sprintf("Support bundle status: %s", sbStatus))
			if sbStatus != "in progress" {
				return nil
			}
		}
	}
}

func getBundleStatus(bundleID BundleID, client downloadSupportBundleHTTPClient) (string, error) {
	log.Debug(fmt.Sprintf("Attempting to get status for support bundle %s", bundleID))
	statusCode, body, err := client.GetSupportBundleStatus(string(bundleID))
	if err != nil {
		return "", err
	}

	log.Debug(fmt.Sprintf("Got HTTP response status: %d", statusCode))
	if statusCode != http.StatusOK {
		return "", fmt.Errorf("http request failed with: %d %s", statusCode, http.StatusText(statusCode))
	}

	parsedBody, err := flunkyhttp.ParseJSON(body)
	if err != nil {
		return "", err
	}

	sbStatus, err := parsedBody.GetString("status")
	if err != nil {
		return "", err
	}
	return sbStatus, nil
}

func handleClose(closer io.Closer) {
	if closer != nil {
		err := closer.Close()
		if err != nil {
			log.Warn("error occurred while closing: %+v", err)
		}
	}
}
