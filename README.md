# KVM Java Launcher

Single-file Windows launcher for old Java-based BMC KVM consoles.

Current profiles:

- `ami` - old AMI MegaRAC / JViewer consoles.
- `supermicro` - tries common Supermicro JNLP endpoints and runs the
  downloaded graphical iKVM JNLP with matching native libraries.
- `supermicro-sol` - starts Supermicro Serial-over-LAN Java console JNLP.
- `generic` - tries common JNLP endpoints used by vendor BMC consoles.
- `dell`, `hp`, `hpe`, `lenovo`, `ibm` - experimental vendor JNLP discovery.
- `auto` - detects AMI MegaRAC from the web page, detects Supermicro when the
  landing page says so, otherwise tries generic JNLP.
- `manual` - starts a local JNLP file through the built-in Java/JAR runner.

The Windows build embeds Eclipse Temurin Java 8 x64 and extracts it to the
user cache on first run. Java does not need to be installed separately.

Examples:

```bat
KVM-Java-Launcher.exe -target 10.168.68.107 -user admin -password admin -profile ami
KVM-Java-Launcher.exe -target 10.168.68.120 -user ADMIN -password ADMIN -profile auto
KVM-Java-Launcher.exe -target 10.168.68.121 -user ADMIN -password ADMIN -profile supermicro
KVM-Java-Launcher.exe -target 10.168.68.122 -user root -password calvin -profile dell
KVM-Java-Launcher.exe -target 90.189.216.197 -host-header 192.168.7.70 -user root -password calvin -profile dell
KVM-Java-Launcher.exe -profile manual -jnlp C:\Users\Admin\Downloads\launch.jnlp
```

If started without arguments, the program asks for host, user, password, and
profile interactively.

NAT notes:

- AMI and Supermicro Java consoles may need more than the BMC web port.
- Dell iDRAC, HPE iLO, and Lenovo XCC usually require the HTTPS management
  port plus a dedicated remote-console port.
- If using DST-NAT, prefer forwarding the same external and internal port
  numbers. Some vendor launchers embed host/port values in the downloaded
  launch file and break when ports are remapped.
- Some BMC web servers reject NAT access when the HTTP `Host` header is the
  external address. Use `-host-header <internal-bmc-host>` for that case.
