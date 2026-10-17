# Setting up a soju bouncer

[soju](https://soju.im) is a small IRC bouncer: it stays connected to your networks 24/7, keeps the history, and lets IRChatterbox (and your phone) pick up where you left off. This guide sets it up on a Debian or Ubuntu VM or LXC container. It's a single Go binary, and 512 MB of RAM and 1 CPU is plenty.

You'll end up with:

- soju listening with TLS on `ircs://bnc.example.com:6697`
- one soju account (`andrew`) with your networks (Libera, etc.) added to it
- IRChatterbox connecting to it with **Through a bouncer**

Replace `bnc.example.com` with your own hostname throughout.

---

## 1. DNS and firewall

1. Point an A (and AAAA, if you have IPv6) record for `bnc.example.com` at the server.
2. Open TCP **6697** inbound. If the machine is behind NAT on the Proxmox host, forward 6697 to the VM.
3. Open TCP **80** too if you'll use Certbot's standalone mode in step 3 (only needed while getting or renewing the certificate).

## 2. Install soju

soju doesn't ship prebuilt binaries, so you build it with Go. Distro Go is often too old; soju needs **Go 1.24 or newer**.

```bash
# Go (check https://go.dev/dl/ for the current version)
cd /tmp
curl -LO https://go.dev/dl/go1.26.9.linux-amd64.tar.gz
rm -rf /usr/local/go && tar -C /usr/local -xzf go1.26.9.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# soju, built with the pure-Go SQLite driver (no C compiler needed)
GOBIN=/usr/local/bin go install -tags=moderncsqlite codeberg.org/emersion/soju/cmd/...@latest
soju -h    # prints the options if it worked
```

This installs `soju` (the bouncer), `sojuctl` (admin commands) and `sojudb` (offline database tool) into `/usr/local/bin`.

To update later, run the same `go install` line again and `systemctl restart soju`.

## 3. TLS certificate

The simplest option is Certbot in standalone mode (port 80 must be free and open):

```bash
apt install certbot
certbot certonly --standalone -d bnc.example.com
```

If something on the box already serves port 80 (nginx), use `certbot certonly --webroot` or your usual method instead. Any valid certificate for the hostname works.

## 4. User and config

```bash
useradd --system --home /var/lib/soju --create-home --shell /usr/sbin/nologin soju
mkdir -p /etc/soju

# let soju read the certificate
chmod 0755 /etc/letsencrypt/{live,archive}
chgrp soju /etc/letsencrypt/archive/bnc.example.com/privkey*.pem
chmod 0640 /etc/letsencrypt/archive/bnc.example.com/privkey*.pem
```

Create `/etc/soju/config`:

```
hostname bnc.example.com
listen ircs://:6697
listen unix+admin:///run/soju/admin
tls /etc/letsencrypt/live/bnc.example.com/fullchain.pem /etc/letsencrypt/live/bnc.example.com/privkey.pem
db sqlite3 /var/lib/soju/soju.db
message-store db
```

`message-store db` keeps history in the database. IRChatterbox needs it, because that's what lets it fetch what you missed and sync read markers.

## 5. Run it as a service

Create `/etc/systemd/system/soju.service`:

```ini
[Unit]
Description=soju IRC bouncer
Wants=network-online.target
After=network-online.target

[Service]
User=soju
Group=soju
ExecStart=/usr/local/bin/soju -config /etc/soju/config
ExecReload=/bin/kill -HUP $MAINPID
RuntimeDirectory=soju
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=/var/lib/soju
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

Create your account and start the service:

```bash
sudo -u soju sojudb -config /etc/soju/config create-user andrew -admin   # asks for a password
systemctl daemon-reload
systemctl enable --now soju
systemctl status soju       # should say "active (running)"
journalctl -u soju -f       # live log
```

Reload soju after the certificate renews:

```bash
cat > /etc/letsencrypt/renewal-hooks/post/soju.sh <<'EOF'
#!/bin/sh
chgrp soju /etc/letsencrypt/archive/bnc.example.com/privkey*.pem
chmod 0640 /etc/letsencrypt/archive/bnc.example.com/privkey*.pem
systemctl reload soju
EOF
chmod 755 /etc/letsencrypt/renewal-hooks/post/soju.sh
```

## 6. Add your networks

Networks are added by messaging **BouncerServ**, soju's built-in service. Do it from IRChatterbox:

1. **Add server** → **Through a bouncer**
   - Host `bnc.example.com`, port `6697`, TLS on
   - Bouncer type **soju**, username `andrew`, your soju password
   - **Network: leave empty** for now
2. Connect. You're now talking to soju itself. In the server tab, add each network:

   ```
   /msg BouncerServ network create -addr ircs://irc.libera.chat -name libera -nick Andrew
   /msg BouncerServ sasl set-plain -network libera Andrew YourNickServPassword
   /msg BouncerServ network update libera
   ```

   The last line reconnects so the SASL login takes effect. For a network without SASL, use `-connect-command "PRIVMSG NickServ :IDENTIFY password"` on `network create` instead.

   Check with `/msg BouncerServ network status`.

3. Edit that server in IRChatterbox and set **Network** to `libera`, or add a new server per network. Each one connects to the same bouncer with a different network name.
4. Join your channels as usual. soju remembers them and rejoins on its own after restarts.

You can delete the empty "no network" server once your networks are set up, or keep it as a handy console for BouncerServ commands (`/msg BouncerServ help`).

## 7. Your phone

Install **Goguma** (Android) and add `bnc.example.com:6697` with the same soju username and password. It finds all your networks automatically and shares history and read markers with IRChatterbox.

---

## Good to know

- **Auto-away:** soju marks you away on a network when every client has disconnected. To turn it off: `/msg BouncerServ network update libera -auto-away false`.
- **Backups:** everything (account, networks, channels, history) is in `/var/lib/soju/soju.db`. Back up that one file, ideally with `sqlite3 /var/lib/soju/soju.db ".backup /path/backup.db"` while soju runs.
- **Admin from the shell:** `sojuctl -config /etc/soju/config user status`, or `... user create -username alice -password ...` to add another person.
- **Logs:** `journalctl -u soju`. If clients can't connect, check the firewall and that the certificate paths in the config are readable by the `soju` user.
- **Security:** soju is the thing that holds your network passwords, so give it a strong password, keep it updated, and expose only port 6697.

## Troubleshooting

| Symptom | Fix |
|---|---|
| IRChatterbox: "Bouncer login failed" | Wrong username or password, or the network name doesn't exist (`network status` lists them) |
| Connects but no channels | The network name is empty or wrong, so you're talking to soju itself rather than a network |
| `tls: failed to load key pair` in the log | The `soju` user can't read `privkey.pem`. Redo the `chgrp`/`chmod` from step 4 |
| No missed messages after reconnecting | `message-store` isn't `db` (the `memory` store doesn't support history) |
| Port 6697 refused | Firewall or NAT forward missing, or soju isn't running (`systemctl status soju`) |
