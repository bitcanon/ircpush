package cmd

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"

	appcfg "github.com/bitcanon/ircpush/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var genCmd = &cobra.Command{
	Use:   "gen",
	Short: "Generate and send test data to the ircpush serve input",
	Long:  "Generate synthetic logs in various formats (CheckMK, Linux syslog, Cisco, RouterOS, Juniper, FortiGate, PaloAlto, HAProxy, Nginx, Postfix, SSHD, Windows) and send them to the TCP input.",
	RunE: func(cmd *cobra.Command, args []string) error {
		formatsCSV, _ := cmd.Flags().GetString("formats")
		rate, _ := cmd.Flags().GetDuration("rate")
		count, _ := cmd.Flags().GetInt("count")
		target, _ := cmd.Flags().GetString("target")
		chPrefix, _ := cmd.Flags().GetString("channel")
		randomize, _ := cmd.Flags().GetBool("randomize")
		jitterPct, _ := cmd.Flags().GetFloat64("jitter")
		levelsCSV, _ := cmd.Flags().GetString("levels")

		if jitterPct < 0 {
			jitterPct = 0
		}
		if jitterPct > 1 {
			jitterPct = 1
		}

		// Load effective config to get default listen target.
		var cfg appcfg.Config
		_ = viper.Unmarshal(&cfg)

		if target == "" {
			if cfg.TCP.Listen != "" {
				target = cfg.TCP.Listen
			} else {
				target = ":9000"
			}
		}
		if strings.HasPrefix(target, ":") {
			target = "127.0.0.1" + target
		}

		fmts := parseCSV(formatsCSV)
		if len(fmts) == 0 {
			fmts = []string{
				"syslog", "cisco", "routeros", "checkmk",
				"juniper", "fortigate", "paloalto",
				"haproxy", "nginx", "postfix", "sshd", "windows",
			}
		}

		levels := parseCSV(levelsCSV)
		if len(levels) == 0 {
			levels = []string{
				"trace", "debug", "info", "notice",
				"warn", "warning", "error", "err", "critical", "crit", "alert", "emerg",
				"ok", "success", "passed", "recovered", "resolved", "online",
				"fail", "failed", "down", "offline", "degraded", "timeout",
				"drop", "dropped", "deny", "denied", "block", "blocked",
				"allow", "allowed", "permit", "permitted",
				"issue", "problem", "incident",
				"restart", "reboot",
				"up",
			}
		}

		// Seed RNG
		rand.Seed(time.Now().UnixNano())

		// Always TCP
		conn, err := net.Dial("tcp", target)
		if err != nil {
			return fmt.Errorf("dial tcp %s: %w", target, err)
		}
		defer conn.Close()

		fmt.Fprintf(
			os.Stderr,
			"Sending test data: target=%s rate=%s count=%d formats=%v channel=%q\n",
			target, rate, count, fmts, chPrefix,
		)

		// Build generators; choose severity/keyword per message
		genFns := make([]func() string, 0, len(fmts))
		for _, f := range fmts {
			switch strings.ToLower(strings.TrimSpace(f)) {
			case "syslog":
				genFns = append(genFns, func() string { return genLinuxSyslog(pick(levels...)) })
			case "cisco":
				genFns = append(genFns, func() string { return genCisco(pick(levels...)) })
			case "routeros":
				genFns = append(genFns, func() string { return genRouterOS(pick(levels...)) })
			case "checkmk":
				genFns = append(genFns, func() string { return genCheckMK(pick(levels...)) })
			case "juniper":
				genFns = append(genFns, func() string { return genJuniper(pick(levels...)) })
			case "fortigate":
				genFns = append(genFns, func() string { return genFortiGate(pick(levels...)) })
			case "paloalto":
				genFns = append(genFns, func() string { return genPaloAlto(pick(levels...)) })
			case "haproxy":
				genFns = append(genFns, func() string { return genHAProxy(pick(levels...)) })
			case "nginx":
				genFns = append(genFns, func() string { return genNginx(pick(levels...)) })
			case "postfix":
				genFns = append(genFns, func() string { return genPostfix(pick(levels...)) })
			case "sshd":
				genFns = append(genFns, func() string { return genSSHD(pick(levels...)) })
			case "windows":
				genFns = append(genFns, func() string { return genWindows(pick(levels...)) })
			default:
				return fmt.Errorf("unknown format: %s", f)
			}
		}

		send := func(line string) error {
			if chPrefix != "" {
				line = chPrefix + " " + line
			}
			if !strings.HasSuffix(line, "\n") {
				line += "\n"
			}
			_, err := conn.Write([]byte(line))
			return err
		}

		sent := 0
		for {
			var line string
			if randomize {
				line = genFns[rand.Intn(len(genFns))]()
			} else {
				line = genFns[sent%len(genFns)]()
			}
			if err := send(line); err != nil {
				return fmt.Errorf("send: %w", err)
			}
			sent++
			if count > 0 && sent >= count {
				break
			}
			// Apply jitter to rate if requested
			sleep := rate
			if jitterPct > 0 {
				delta := time.Duration(float64(rate) * jitterPct)
				sleep = rate - delta + time.Duration(rand.Int63n(int64(2*delta)+1))
				if sleep <= 0 {
					sleep = time.Millisecond
				}
			}
			time.Sleep(sleep)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(genCmd)

	genCmd.Flags().String("formats",
		"syslog,cisco,routeros,checkmk,juniper,fortigate,paloalto,haproxy,nginx,postfix,sshd,windows",
		"comma-separated formats")
	genCmd.Flags().String("levels",
		"trace,debug,info,notice,warn,warning,error,err,critical,crit,alert,emerg,ok,success,passed,recovered,resolved,online,fail,failed,down,offline,degraded,timeout,drop,dropped,deny,denied,block,blocked,allow,allowed,permit,permitted,issue,problem,incident,restart,reboot,up",
		"comma-separated severities/keywords to mix into messages")
	genCmd.Flags().Duration("rate", time.Second, "send interval (e.g. 500ms, 1s)")
	genCmd.Flags().Int("count", 0, "number of messages to send (0=infinite)")
	genCmd.Flags().String("target", "", "override TCP target address (defaults to tcp.listen from config)")
	genCmd.Flags().String("channel", "", "optional channel prefix (e.g. #ndc-dev)")
	genCmd.Flags().Bool("randomize", true, "pick formats randomly instead of round-robin")
	genCmd.Flags().Float64("jitter", 0.2, "sleep jitter as fraction of rate (0..1)")
}

// --- Helpers and generators with more variance ---

func parseCSV(csv string) []string {
	parts := strings.Split(csv, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func pick[T any](vals ...T) T { return vals[rand.Intn(len(vals))] }

func randIPv4() string {
	return fmt.Sprintf("%d.%d.%d.%d", 1+rand.Intn(223), rand.Intn(255), rand.Intn(255), 1+rand.Intn(254))
}
func randPort() int { return 1024 + rand.Intn(64511) }
func randMAC() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	b[0] &= 0xfe
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
}
func randIf() string {
	return pick("ether1", "ether2", "vlan10", "vlan20", "bond0", "ge-0/0/1", "Gi0/1")
}
func randVLAN() int { return pick(1, 10, 20, 100, 200, 4094) }
func randASN() int  { return pick(64512, 64513, 65001, 65010, 65100) }
func randBGPPeer() string {
	return fmt.Sprintf("%s ASN%d", randIPv4(), randASN())
}
func randUser() string   { return pick("bitcanon", "root", "deploy", "www-data", "admin", "svc-check") }
func randDomain() string { return pick("example.com", "example.se", "corp.local", "internal.net") }
func randURL() string {
	host := pick("web01", "api", "cdn") + "." + randDomain()
	path := pick("/index.html", "/healthz", "/login", "/api/v1/items", "/status")
	return "https://" + host + path
}
func randHTTPStatus() int {
	return pick(200, 201, 204, 301, 302, 400, 401, 403, 404, 408, 429, 500, 502, 503)
}
func randMs() int { return pick(2, 5, 12, 20, 37, 50, 73, 120, 250, 480, 900, 1500) }

// Linux syslog-ish with varied severities and messages
func genLinuxSyslog(sev string) string {
	host := pick("ndc-syslog01", "web01", "db01", "proxy02")
	prog := pick("sudo", "sshd", "systemd", "kernel", "nginx", "auditd", "cron", "rsyslogd")
	user := randUser()
	msg := ""

	switch sev {
	case "trace", "debug", "info", "notice", "ok", "success", "online", "up", "recovered", "resolved", "passed":
		msg = pick(
			"service rsyslog.service started successfully",
			"user %s authenticated via ssh",
			"interface %s is up",
			"rotation complete for /var/log/syslog",
			"CPU load normal",
			"unit %s: state is active (running)",
			"job completed without error",
		)
	case "warn", "warning", "issue", "problem", "degraded", "timeout":
		msg = pick(
			"disk usage high on /var: 92%%",
			"unit %s failed to start, retrying",
			"interface %s is down",
			"ssh: possible brute force from %s",
			"connection reset by peer",
			"request timed out after %dms",
			"journal overflow: dropping old messages",
		)
	default: // error, critical, alert, emerg, fail, failed, down, offline, err
		msg = pick(
			"kernel: BUG: soft lockup on CPU 0",
			"segfault at 0000 ip 0000 sp 0000 error 4 in %s",
			"OOM killer invoked",
			"filesystem read-only, remount required",
			"panic: fatal exception",
			"RAID array degraded",
		)
	}

	// Fill placeholders if present
	msg = fmt.Sprintf(msg, pick("rsyslog.service", "nginx", "postgres"), randIf(), randIPv4(), randMs())

	// Include classic sudo line sometimes to exercise your highlight rules
	if prog == "sudo" && rand.Intn(5) == 0 {
		return fmt.Sprintf("%s: %s   %s : TTY=pts/0 ; PWD=/home/%s ; USER=root ; COMMAND=/usr/bin/systemctl restart rsyslog.service",
			host, prog, user, user)
	}

	levelTag := strings.ToUpper(sev)
	return fmt.Sprintf("%s %s[%d]: %s: %s", host, prog, 1000+rand.Intn(8000), levelTag, msg)
}

// Cisco IOS/ASA style with multiple facilities and up/down
func genCisco(sev string) string {
	host := pick("ios-rtr01", "asa-fw01", "cat9300-1")
	iface := randIf()
	srcIP, dstIP := randIPv4(), randIPv4()
	sport, dport := randPort(), pick(22, 80, 443, 3389)
	mac := randMAC()

	switch sev {
	case "up", "ok", "info", "notice", "success", "resolved", "online":
		return pick(
			fmt.Sprintf("%s: %%LINK-3-UPDOWN: Interface %s, changed state to up", host, iface),
			fmt.Sprintf("%s: %%LINEPROTO-5-UPDOWN: Line protocol on Interface %s, changed state to up", host, iface),
			fmt.Sprintf("%s: %%SYS-5-CONFIG_I: Configured from console by vty0", host),
			fmt.Sprintf("%s: %%SPAN-4-PORTUP: Port %s forward enabled", host, iface),
		)
	case "down", "fail", "failed", "warn", "warning", "issue", "problem", "degraded":
		return pick(
			fmt.Sprintf("%s: %%LINK-3-UPDOWN: Interface %s, changed state to down", host, iface),
			fmt.Sprintf("%s: %%PM-4-ERR_DISABLE: link-flap error detected on %s, putting %s in err-disable state", host, iface, iface),
			fmt.Sprintf("%s: %%SEC-6-IPACCESSLOGP: list ACL-WARN denied tcp %s:%d -> %s:%d flags RST", host, srcIP, sport, dstIP, dport),
			fmt.Sprintf("%s: %%STP-5-ROOTCHANGE: Root switch for VLAN %d has changed", host, randVLAN()),
		)
	default: // error, critical, alert, emerg, drop, deny, blocked
		return pick(
			fmt.Sprintf("%s: %%SEC-6-IPACCESSLOGP: list ACL-DROP dropped tcp %s:%d -> %s:%d flags SYN", host, srcIP, sport, dstIP, dport),
			fmt.Sprintf("%s: %%SPANTREE-2-LOOPGUARD_BLOCK: Loop guard blocking port %s on VLAN%04d.", host, iface, randVLAN()),
			fmt.Sprintf("%s: %%DOT1X-5-FAIL: Authentication failed on %s for client %s", host, iface, mac),
			fmt.Sprintf("%s: %%BGP-3-NOTIFICATION: %s neighbor reset (timeout)", host, randBGPPeer()),
		)
	}
}

// RouterOS firewall and interface events
func genRouterOS(sev string) string {
	host := pick("fw2", "ros-core", "edge01")
	inIf, outIf := randIf(), pick("(unknown 0)", randIf())
	srcIP, dstIP := randIPv4(), randIPv4()
	sport, dport := randPort(), pick(53, 80, 123, 443)
	flags := pick("SYN", "ACK", "FIN", "RST", "PSH", "URG")
	mac := randMAC()
	proto := pick("tcp", "udp", "icmp")

	switch sev {
	case "up", "ok,success", "info", "notice", "resolved", "online":
		return pick(
			fmt.Sprintf("%s: accept output: in:%s out:%s, proto %s, %s:%d->%s:%d, mac:%s, hop=64", host, inIf, outIf, proto, srcIP, sport, dstIP, dport, mac),
			fmt.Sprintf("%s: interface %s link up", host, inIf),
			fmt.Sprintf("%s: dhcp: lease granted %s to %s", host, srcIP, mac),
			fmt.Sprintf("%s: route added: dst=%s/32 gw=%s", host, dstIP, srcIP),
		)
	case "down", "fail", "failed", "warn", "warning", "issue", "problem", "degraded", "timeout":
		return pick(
			fmt.Sprintf("%s: drop input: in:%s out:%s, proto %s, %s:%d->%s:%d, mac:%s, flag=%s", host, inIf, outIf, proto, srcIP, sport, dstIP, dport, mac, flags),
			fmt.Sprintf("%s: interface %s link down", host, inIf),
			fmt.Sprintf("%s: queue tree %s warning: limit reached", host, pick("wan-queue", "lan-queue")),
			fmt.Sprintf("%s: dhcp: request timeout for %s", host, srcIP),
		)
	default: // error, critical, alert, drop, deny, blocked, offline
		return pick(
			fmt.Sprintf("%s: drop forward: in:%s out:%s, proto %s, %s:%d->%s:%d, mac:%s, flag=%s", host, inIf, outIf, proto, srcIP, sport, dstIP, dport, mac, flags),
			fmt.Sprintf("%s: hotspot fatal: radius timeout for %s", host, srcIP),
			fmt.Sprintf("%s: disk error: write failed on nand", host),
			fmt.Sprintf("%s: firewall: deny rule matched from %s to %s", host, srcIP, dstIP),
		)
	}
}

// CheckMK notification-like with varied states
func genCheckMK(sev string) string {
	host := pick("db01", "web01", "cache02", "mq01")
	service := pick("CPU load", "Filesystem /", "Memory", "Interface eth0", "PostgreSQL", "NTP", "Ping")
	state := map[string]string{
		"ok":        "OK",
		"success":   "OK",
		"info":      "OK",
		"notice":    "OK",
		"warning":   "WARN",
		"warn":      "WARN",
		"issue":     "WARN",
		"problem":   "WARN",
		"error":     "CRIT",
		"critical":  "CRIT",
		"crit":      "CRIT",
		"fail":      "CRIT",
		"failed":    "CRIT",
		"down":      "CRIT",
		"up":        "OK",
		"recovered": "OK",
	}[strings.ToLower(sev)]
	if state == "" {
		state = "OK"
	}

	detail := ""
	switch service {
	case "CPU load":
		detail = pick("OK - 1 min load 0.23 at 4 CPUs", "WARN - 1 min load 3.7 at 4 CPUs", "CRIT - 1 min load 7.9 at 4 CPUs")
	case "Filesystem /":
		detail = pick("OK - 20% used", "WARN - 88% used", "CRIT - 97% used")
	case "Memory":
		detail = pick("OK - 35% used", "WARN - 82% used", "CRIT - 95% used")
	case "Interface eth0":
		detail = pick("OK - link up", "WARN - errors detected", "CRIT - link down")
	case "NTP":
		detail = pick("OK - offset 0.2ms", "WARN - offset 89ms", "CRIT - unsynchronized")
	case "PostgreSQL":
		detail = pick("OK - connections 12", "WARN - slow queries detected", "CRIT - database not responding")
	default:
		detail = pick("OK - all checks passed", "WARN - response time high", "CRIT - service not responding")
	}

	return fmt.Sprintf("Check_MK[%d]: %s;SERVICE %s;%s - %s",
		10000+rand.Intn(90000), host, service, state, detail)
}

// Juniper-like logs (interfaces, BGP, OSPF)
func genJuniper(sev string) string {
	host := pick("jnp-ex4300-1", "jnp-mx204-1")
	iface := pick("xe-0/0/1", "ge-0/0/10", "ae1")
	switch sev {
	case "up", "ok", "info", "notice", "resolved":
		return pick(
			fmt.Sprintf("%s mgd[1234]: UI_COMMIT: User 'admin' committed configuration", host),
			fmt.Sprintf("%s fpc0 %s: %s: up", host, iface, iface),
			fmt.Sprintf("%s rpd[5678]: RPD_BGP_NEIGHBOR_STATE_CHANGED: %s (Established)", host, randBGPPeer()),
		)
	case "warn", "warning", "issue", "problem", "degraded":
		return pick(
			fmt.Sprintf("%s chassisd[2222]: CHASSISD_FRU_OFFLINE_NOTICE: FPC 0 is going offline", host),
			fmt.Sprintf("%s rpd[5678]: RPD_OSPF_NEIGHBOR_CHANGE: Neighbor %s state Dropping", host, randIPv4()),
			fmt.Sprintf("%s fpc0 %s: %s: rx errors detected", host, iface, iface),
		)
	default:
		return pick(
			fmt.Sprintf("%s rpd[5678]: RPD_BGP_NEIGHBOR_DOWN: %s (Hold timer expired)", host, randBGPPeer()),
			fmt.Sprintf("%s craftd[9999]: ALARM_MAJOR: Power supply failure", host),
			fmt.Sprintf("%s kernel: panic: fatal exception", host),
		)
	}
}

// FortiGate-like UTM logs
func genFortiGate(sev string) string {
	host := pick("fg-60e-1", "fg-100f-2")
	srcIP, dstIP := randIPv4(), randIPv4()
	act := pick("accept", "deny", "drop", "monitor")
	virus := pick("EICAR_Test_File", "Trojan.Generic", "W64.Malware", "JS.Miner")
	switch sev {
	case "info", "notice", "ok", "success":
		return fmt.Sprintf("%s date=2025-11-05 time=08:25:32 devname=%s logid=0000000013 type=traffic subtype=forward level=notice action=%s srcip=%s dstip=%s service=https duration=%ds sent=1234 rcvd=5678", host, host, act, srcIP, dstIP, rand.Intn(30))
	case "warn", "warning", "issue", "problem":
		return fmt.Sprintf("%s date=2025-11-05 time=08:25:32 devname=%s type=utm subtype=webfilter level=warning action=blocked srcip=%s dstip=%s url=%s", host, host, srcIP, dstIP, randURL())
	default:
		return fmt.Sprintf("%s date=2025-11-05 time=08:25:32 devname=%s type=utm subtype=virus level=critical action=blocked srcip=%s dstip=%s virus=%s", host, host, srcIP, dstIP, virus)
	}
}

// Palo Alto-like threat/traffic
func genPaloAlto(sev string) string {
	device := pick("pa-220-1", "pa-850-2")
	srcIP, dstIP := randIPv4(), randIPv4()
	app := pick("ssl", "web-browsing", "ssh", "dns")
	act := pick("allow", "deny", "reset-both", "reset-server")
	switch sev {
	case "info", "notice", "ok", "success":
		return fmt.Sprintf("%s TRAFFIC end src=%s dst=%s app=%s action=%s bytes=12345 elapsed=%d", device, srcIP, dstIP, app, act, randMs())
	case "warn", "warning", "issue", "problem":
		return fmt.Sprintf("%s THREAT url src=%s dst=%s category=phishing url=%s action=blocked severity=medium", device, srcIP, dstIP, randURL())
	default:
		return fmt.Sprintf("%s THREAT vulnerability src=%s dst=%s threat=buffer-overflow action=blocked severity=critical", device, srcIP, dstIP)
	}
}

// HAProxy access with statuses
func genHAProxy(sev string) string {
	fe := pick("frontend_www", "api_fe")
	be := pick("backend_www", "api_be")
	srv := pick("web01", "web02", "api01")
	code := randHTTPStatus()
	ms := randMs()
	verb := pick("GET", "POST", "PUT", "DELETE")
	url := randURL()
	switch sev {
	case "info", "ok", "success", "notice":
		return fmt.Sprintf("haproxy: %s %s/%s %d %dms %s %s", fe, be, srv, code, ms, verb, url)
	case "warn", "warning", "degraded", "issue":
		return fmt.Sprintf("haproxy: %s %s/%s %d %dms RETRY %s %s", fe, be, srv, pick(408, 429, 500), ms, verb, url)
	default:
		return fmt.Sprintf("haproxy: %s %s/%s %d %dms DOWN %s %s", fe, be, srv, 503, ms, verb, url)
	}
}

// Nginx access/error
func genNginx(sev string) string {
	host := pick("nginx01", "nginx02")
	code := randHTTPStatus()
	ms := randMs()
	url := randURL()
	switch sev {
	case "info", "ok", "success", "notice":
		return fmt.Sprintf("%s: access: %s %d %dms", host, url, code, ms)
	case "warn", "warning", "issue", "problem", "degraded":
		return fmt.Sprintf("%s: error: upstream timed out (110: Connection timed out) while reading response header from upstream, url: %s", host, url)
	default:
		return fmt.Sprintf("%s: error: connect() failed (111: Connection refused) while connecting to upstream, url: %s", host, url)
	}
}

// Postfix mail logs
func genPostfix(sev string) string {
	host := pick("mx1", "mx2")
	queueID := fmt.Sprintf("%X", 100000+rand.Intn(0xFFFFF))
	from := fmt.Sprintf("noreply@%s", randDomain())
	to := fmt.Sprintf("user@%s", randDomain())
	switch sev {
	case "info", "ok", "success", "notice":
		return fmt.Sprintf("%s postfix/qmgr[%d]: %s: from=<%s>, size=1234, nrcpt=1 (queue active)", host, 1000+rand.Intn(9000), queueID, from)
	case "warn", "warning", "issue", "problem":
		return fmt.Sprintf("%s postfix/smtp[%d]: %s: to=<%s>, relay=%s[%.15s]:25, delay=2.3, delays=0.1/0/0.3/1.9, dsn=4.2.0, status=deferred (host mx.%s said: 450 try again later)", host, 1000+rand.Intn(9000), queueID, to, "mx."+randDomain(), randIPv4(), randDomain())
	default:
		return fmt.Sprintf("%s postfix/smtp[%d]: %s: to=<%s>, relay=%s[%.15s]:25, delay=0.5, dsn=5.7.1, status=bounced (554 5.7.1 relay access denied)", host, 1000+rand.Intn(9000), queueID, to, "mx."+randDomain(), randIPv4())
	}
}

// SSHD auth logs
func genSSHD(sev string) string {
	host := pick("auth01", "web01")
	user := randUser()
	ip := randIPv4()
	switch sev {
	case "info", "ok", "success", "notice":
		return fmt.Sprintf("%s sshd[%d]: Accepted publickey for %s from %s port %d ssh2", host, 1000+rand.Intn(9000), user, ip, randPort())
	case "warn", "warning", "issue", "problem":
		return fmt.Sprintf("%s sshd[%d]: Failed password for %s from %s port %d ssh2", host, 1000+rand.Intn(9000), user, ip, randPort())
	default:
		return fmt.Sprintf("%s sshd[%d]: Disconnecting %s %s port %d: Too many authentication failures", host, 1000+rand.Intn(9000), user, ip, randPort())
	}
}

// Windows event-like (flattened)
func genWindows(sev string) string {
	host := pick("WIN-APP01", "WIN-DB02")
	src := pick("Service Control Manager", "Schannel", "Kernel-General", "Netlogon", "MSFTPSVC")
	eventID := pick(7036, 7031, 36887, 6005, 5722, 10016)
	switch sev {
	case "info", "ok", "success", "notice":
		return fmt.Sprintf("%s: %s EventID %d: The service entered the running state.", host, src, eventID)
	case "warn", "warning", "issue", "problem", "degraded":
		return fmt.Sprintf("%s: %s EventID %d: The service terminated unexpectedly. This has happened %d time(s).", host, src, eventID, 1+rand.Intn(5))
	default:
		return fmt.Sprintf("%s: %s EventID %d: A fatal alert was generated and sent to the remote endpoint. The TLS protocol defined fatal alert code is 40.", host, src, eventID)
	}
}
