// Command smoke-image verifies a locally built image using the Docker CLI.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

func docker(args ...string) (string, error) {
	b, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, b)
	}
	return strings.TrimSpace(string(b)), nil
}
func main() {
	image := flag.String("image", "", "local image tag")
	platform := flag.String("platform", "linux/amd64", "target platform")
	flag.Parse()
	if *image == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*image, *platform); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(image, platform string) error {
	return runWithDocker(image, platform, docker)
}

func runWithDocker(image, platform string, docker func(...string) (string, error)) error {
	name := fmt.Sprintf("wppgo-smoke-%d", time.Now().UnixNano())
	volume := name + "-data"
	if _, err := docker("volume", "create", volume); err != nil {
		return err
	}
	defer func() {
		logs, _ := docker("logs", name)
		fmt.Println(logs)
		docker("rm", "-f", name)
		docker("volume", "rm", volume)
	}()
	if _, err := docker("run", "-d", "--name", name, "--platform", platform, "-e", "PORT=21466", "-e", "SECRET_KEY=smoke-secret", "-e", "SHUTDOWN_TIMEOUT=10s", "-p", "127.0.0.1::21466", "-v", volume+":/data", image); err != nil {
		return err
	}
	base, err := publishedURL(docker, name)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	call := func(method, path, token string) (map[string]any, error) {
		req, err := http.NewRequest(method, base+path, nil)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("%s: HTTP %d %s", path, resp.StatusCode, b)
		}
		var data map[string]any
		if len(bytes.TrimSpace(b)) > 0 {
			err = json.Unmarshal(b, &data)
		}
		return data, err
	}
	healthy := func() error {
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			status, e := docker("inspect", "--format", "{{.State.Health.Status}}", name)
			if e != nil {
				return e
			}
			if status == "healthy" {
				return nil
			}
			time.Sleep(time.Second)
		}
		return errors.New("container did not become healthy")
	}
	if err = healthy(); err != nil {
		return err
	}
	if _, err = call("GET", "/readyz", ""); err != nil {
		return err
	}
	if _, err = docker("exec", name, "/app/server", "-healthcheck"); err != nil {
		return err
	}
	if _, err = docker("exec", "-e", "PORT=1", name, "/app/server", "-healthcheck"); err == nil {
		return errors.New("healthcheck accepted an unreachable API")
	}
	data, err := call("POST", "/api/smoke/smoke-secret/generate-token", "")
	if err != nil {
		return err
	}
	token, ok := data["token"].(string)
	if !ok || token == "" {
		return errors.New("bootstrap returned no token")
	}
	if _, err = call("POST", "/api/smoke/close-session", token); err != nil {
		return err
	}
	data, err = call("GET", "/openapi.json", "")
	if err != nil {
		return err
	}
	paths, ok := data["paths"].(map[string]any)
	if !ok || data["openapi"] != "3.0.3" || len(paths) < 50 {
		return errors.New("OpenAPI document incomplete")
	}
	stop := func() error {
		if _, e := docker("stop", "--time", "15", name); e != nil {
			return e
		}
		code, e := docker("inspect", "--format", "{{.State.ExitCode}}", name)
		if e != nil {
			return e
		}
		if code != "0" {
			return fmt.Errorf("unclean container shutdown: %s", code)
		}
		return nil
	}
	if err = stop(); err != nil {
		return err
	}
	if _, err = docker("start", name); err != nil {
		return err
	}
	if err = healthy(); err != nil {
		return err
	}
	// Docker may assign a different random host port when the container restarts.
	base, err = publishedURL(docker, name)
	if err != nil {
		return err
	}
	client.CloseIdleConnections()
	record, err := docker("exec", name, "cat", "/data/smoke.session.json")
	if err != nil {
		return err
	}
	var persisted struct {
		Name    string
		Restore bool
	}
	if err = json.Unmarshal([]byte(record), &persisted); err != nil {
		return err
	}
	if persisted.Name != "smoke" || persisted.Restore {
		return errors.New("restart lost explicit closed-session intent")
	}
	data, err = call("GET", "/api/smoke/status-session", token)
	if err != nil {
		return err
	}
	if data["status"] != "CLOSED" {
		return fmt.Errorf("unexpected restart status: %v", data)
	}
	if err = stop(); err != nil {
		return err
	}
	fmt.Printf("Smoke test passed: %s\n", platform)
	return nil
}

func publishedURL(docker func(...string) (string, error), name string) (string, error) {
	port, err := docker("port", name, "21466/tcp")
	if err != nil {
		return "", err
	}
	_, number, err := net.SplitHostPort(strings.Split(port, "\n")[0])
	if err != nil {
		return "", err
	}
	return "http://127.0.0.1:" + number, nil
}
