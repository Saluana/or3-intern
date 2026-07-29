package connect

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"
)

type ServiceSpec struct {
	Label      string
	User       string
	Group      string
	WorkingDir string
	Binary     string
	ConfigPath string
	StateDir   string
	StdoutPath string
	StderrPath string
}

func CurrentServiceSpec(configPath, stateDir string) (ServiceSpec, error) {
	binary, err := os.Executable()
	if err != nil {
		return ServiceSpec{}, err
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		return ServiceSpec{}, err
	}
	current, err := user.Current()
	if err != nil {
		return ServiceSpec{}, err
	}
	group := current.Gid
	if parsed, err := user.LookupGroupId(current.Gid); err == nil {
		group = parsed.Name
	}
	return ServiceSpec{
		Label:      "chat.or3.connect",
		User:       current.Username,
		Group:      group,
		WorkingDir: filepath.Dir(configPath),
		Binary:     binary,
		ConfigPath: configPath,
		StateDir:   stateDir,
		StdoutPath: filepath.Join(stateDir, "connect.log"),
		StderrPath: filepath.Join(stateDir, "connect-error.log"),
	}, nil
}

func RenderService(spec ServiceSpec, platform string) (string, error) {
	var source string
	switch platform {
	case "darwin":
		source = launchdTemplate
	case "linux":
		source = systemdTemplate
	default:
		return "", fmt.Errorf("automatic background service setup is not supported on %s", platform)
	}
	t, err := template.New("service").Funcs(template.FuncMap{
		"xml": xmlEscape,
		"q":   systemdQuote,
	}).Parse(source)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := t.Execute(&output, spec); err != nil {
		return "", err
	}
	return output.String(), nil
}

func ServiceFilePath(platform string) (string, error) {
	switch platform {
	case "darwin":
		return "/Library/LaunchDaemons/chat.or3.connect.plist", nil
	case "linux":
		return "/etc/systemd/system/or3-connect.service", nil
	default:
		return "", fmt.Errorf("automatic background service setup is not supported on %s", platform)
	}
}

func InstallService(spec ServiceSpec) error {
	body, err := RenderService(spec, runtime.GOOS)
	if err != nil {
		return err
	}
	target, err := ServiceFilePath(runtime.GOOS)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp("", "or3-connect-service-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.WriteString(body); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		_ = runElevated("launchctl", "bootout", "system/"+spec.Label)
		if err := runElevated("install", "-o", "root", "-g", "wheel", "-m", "0644", tempPath, target); err != nil {
			return err
		}
		return runElevated("launchctl", "bootstrap", "system", target)
	case "linux":
		if err := runElevated("install", "-o", "root", "-g", "root", "-m", "0644", tempPath, target); err != nil {
			return err
		}
		if err := runElevated("systemctl", "daemon-reload"); err != nil {
			return err
		}
		return runElevated("systemctl", "enable", "--now", "or3-connect.service")
	default:
		return fmt.Errorf("automatic background service setup is not supported on %s", runtime.GOOS)
	}
}

func UninstallService() error {
	target, err := ServiceFilePath(runtime.GOOS)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		_ = runElevated("launchctl", "bootout", "system/chat.or3.connect")
	case "linux":
		_ = runElevated("systemctl", "disable", "--now", "or3-connect.service")
		_ = runElevated("systemctl", "daemon-reload")
	}
	if _, statErr := os.Stat(target); os.IsNotExist(statErr) {
		return nil
	}
	return runElevated("rm", "-f", target)
}

func runElevated(name string, args ...string) error {
	if os.Geteuid() == 0 {
		return runAttached(name, args...)
	}
	return runAttached("sudo", append([]string{name}, args...)...)
}

func runAttached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}

func systemdQuote(value string) string {
	return strconv.Quote(value)
}

const launchdTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>{{xml .Label}}</string>
  <key>UserName</key><string>{{xml .User}}</string>
  <key>GroupName</key><string>{{xml .Group}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{xml .Binary}}</string>
    <string>--config</string>
    <string>{{xml .ConfigPath}}</string>
    <string>connect</string>
    <string>run</string>
    <string>--state-dir</string>
    <string>{{xml .StateDir}}</string>
  </array>
  <key>WorkingDirectory</key><string>{{xml .WorkingDir}}</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ProcessType</key><string>Background</string>
  <key>StandardOutPath</key><string>{{xml .StdoutPath}}</string>
  <key>StandardErrorPath</key><string>{{xml .StderrPath}}</string>
</dict>
</plist>
`

const systemdTemplate = `[Unit]
Description=OR3 remote connection
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User={{.User}}
Group={{.Group}}
WorkingDirectory={{q .WorkingDir}}
ExecStart={{q .Binary}} --config {{q .ConfigPath}} connect run --state-dir {{q .StateDir}}
Restart=always
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths={{q .StateDir}} {{q .WorkingDir}}

[Install]
WantedBy=multi-user.target
`
