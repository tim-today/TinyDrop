package internal

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

type NetAddress struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Type      string `json:"type"` // "tailscale", "lan", "other"
	URL       string `json:"url"`
	IsPrimary bool   `json:"is_primary"`
}

// GetNetworkAddresses detects local LAN IPs and Tailscale virtual IPs (100.64.0.0/10)
func GetNetworkAddresses(port int) []NetAddress {
	var list []NetAddress
	ifaces, err := net.Interfaces()
	if err != nil {
		return list
	}

	_, cgnatNet, _ := net.ParseCIDR("100.64.0.0/10")

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		nameLower := strings.ToLower(iface.Name)
		// Skip docker/veth/bridge virtual interfaces
		if strings.HasPrefix(nameLower, "docker") ||
			strings.HasPrefix(nameLower, "veth") ||
			strings.HasPrefix(nameLower, "br-") ||
			strings.HasPrefix(nameLower, "vmnet") ||
			strings.HasPrefix(nameLower, "vboxnet") {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}
			ipStr := ip4.String()
			if strings.HasPrefix(ipStr, "169.254.") {
				continue
			}

			addrType := "other"
			displayName := iface.Name
			if cgnatNet != nil && cgnatNet.Contains(ip4) || strings.Contains(nameLower, "tailscale") {
				addrType = "tailscale"
				displayName = "Tailscale (" + iface.Name + ")"
			} else if ip4.IsPrivate() {
				addrType = "lan"
				displayName = "LAN (" + iface.Name + ")"
			}

			list = append(list, NetAddress{
				Name: displayName,
				IP:   ipStr,
				Type: addrType,
				URL:  fmt.Sprintf("http://%s:%d", ipStr, port),
			})
		}
	}

	sort.Slice(list, func(i, j int) bool {
		// Prioritize LAN for primary local scanning, then Tailscale
		p := map[string]int{"lan": 1, "tailscale": 2, "other": 3}
		if p[list[i].Type] != p[list[j].Type] {
			return p[list[i].Type] < p[list[j].Type]
		}
		return list[i].IP < list[j].IP
	})

	if len(list) == 0 {
		list = append(list, NetAddress{
			Name:      "Localhost",
			IP:        "127.0.0.1",
			Type:      "lan",
			URL:       fmt.Sprintf("http://127.0.0.1:%d", port),
			IsPrimary: true,
		})
	} else {
		list[0].IsPrimary = true
	}

	return list
}

// OpenInFileManager opens a file or folder in macOS Finder or Windows Explorer
func OpenInFileManager(targetPath string, isDir bool) error {
	switch runtime.GOOS {
	case "darwin":
		if isDir {
			return exec.Command("open", targetPath).Start()
		}
		return exec.Command("open", "-R", targetPath).Start()
	case "windows":
		if isDir {
			return exec.Command("explorer", targetPath).Start()
		}
		return exec.Command("explorer", "/select,", targetPath).Start()
	default:
		return exec.Command("xdg-open", targetPath).Start()
	}
}

// OpenBrowser opens URL in system default browser
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// PickSystemPaths opens native OS file/folder selection dialog on macOS / Windows
func PickSystemPaths(pickFolder bool) ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		var script string
		if pickFolder {
			script = `set chosen to choose folder with prompt "Select folder(s) to share in TinyDrop:" with multiple selections allowed
set output to ""
repeat with f in chosen
	set output to output & POSIX path of f & "\n"
end repeat
return output`
		} else {
			script = `set chosen to choose file with prompt "Select file(s) to share in TinyDrop:" with multiple selections allowed
set output to ""
repeat with f in chosen
	set output to output & POSIX path of f & "\n"
end repeat
return output`
		}
		out, err := exec.Command("osascript", "-e", script).Output()
		if err != nil {
			return nil, nil // user cancelled
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var res []string
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" {
				res = append(res, strings.TrimRight(l, "/"))
			}
		}
		return res, nil

	case "windows":
		var psScript string
		if pickFolder {
			psScript = `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.FolderBrowserDialog; $f.Description = 'Select folder to share in TinyDrop'; if ($f.ShowDialog() -eq 'OK') { Write-Output $f.SelectedPath }`
		} else {
			psScript = `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.OpenFileDialog; $f.Multiselect = $true; $f.Title = 'Select files to share in TinyDrop'; if ($f.ShowDialog() -eq 'OK') { $f.FileNames | ForEach-Object { Write-Output $_ } }`
		}
		out, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command", psScript).Output()
		if err != nil {
			return nil, nil
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var res []string
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" {
				res = append(res, l)
			}
		}
		return res, nil
	default:
		return nil, fmt.Errorf("please drag and drop files directly on this OS")
	}
}
