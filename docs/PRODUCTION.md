# Production configuration

This document describes the recommended setup for an non-containerized Atlas server.

## System requirements

| Dependency | Version | Notes |
| --- | --- | --- |
| go | 1.19+ | |
| git | | for downloading code |
| gcc | | other C compilers will work fine |
| systemd | 250+ | 250 is needed for credentials to work <br/> 240 is needed for data directory stuff |
| logrotate | 3.18.0+ | older versions may also work |
| zstd | | optional, for logrotate compression |
| sqlite3 | 3.37+ | 3.37 is needed for strict tables |

## Installation

1. Install Atlas.

    ```bash
    # option 1: latest version
    sudo GOBIN=/usr/local/bin go install -v -trimpath github.com/r2northstar/atlas/cmd/...@main
    ```

    ```bash
    # option 2: from local git clone
    git clone https://github.com/r2northstar/atlas
    cd atlas
    sudo GOBIN=/usr/local/bin go install -v -trimpath github.com/r2northstar/atlas/cmd/...
    ```

    To update it later, use the same `go install` command.

2. Install the website files.

    ```bash
    sudo git clone https://github.com/R2Northstar/NorthstarTF /usr/share/northstartf
    ```

    To update it later:

    ```bash
    sudo git -C /usr/share/northstartf pull
    ```

    To automatically update the website periodically:

    ```bash
    sudo nano /etc/systemd/system/northstartf-pull.service
    ```

    ```ini
    [Unit]
    Description=Pull NorthstarTF website

    [Service]
    Type=oneshot

    User=root
    Group=root

    WorkingDirectory=/usr/share/northstartf
    ExecStart=git -c pull.ff=only pull

    ProtectSystem=strict
    ProtectHome=yes
    ReadWritePaths=/usr/share/northstartf
    PrivateTmp=yes
    PrivateMounts=yes
    ```

    ```bash
    sudo nano /etc/systemd/system/northstartf-pull.timer
    ```

    ```ini
    [Unit]
    Description=Periodically pull NorthstarTF website

    [Timer]
    OnCalendar=hourly
    RandomizedDelaySec=5m

    [Install]
    WantedBy=timers.target
    ```

    ```bash
    sudo systemctl enable --now northstartf-pull.timer
    ```


3. Download the [IP2Location](https://lite.ip2location.com) DB5 (or higher) database.

    If you update it later while Atlas is running, you will need to run `/usr/bin/systemctl kill --signal=SIGHUP atlas.service`. 

4. Set up the Atlas service user and group.

    ```bash
    sudo nano /etc/sysusers.d/atlas.conf
    ```

    ```
    u atlas - "Atlas" /var/lib/atlas /usr/sbin/nologin
    ```

    ```bash
    sudo systemd-sysusers
    ```

    ```bash
    # give another user direct read-only log and database access
    sudo usermod -aG atlas youruser
    ```

5. Configure Atlas.

    ```bash
    sudo mkdir /etc/atlas
    sudo nano /etc/atlas/config
    ```

    Example configuration (see [pkg/atlas.Config](../pkg/atlas/config.go) for config docs):

    ```properties
    # listen config
    ATLAS_ADDR=
    ATLAS_ADDR_HTTPS=:443
    ATLAS_CLOUDFLARE=true
    ATLAS_HOST=northstar.tf
    ATLAS_SERVER_CERTS=@northstartf

    # don't need debug or lower log levels
    ATLAS_LOG_LEVEL=info

    # log pretty error+ logs to stdout/journald
    ATLAS_LOG_STDOUT=true
    ATLAS_LOG_STDOUT_PRETTY=true
    ATLAS_LOG_STDOUT_LEVEL=error

    # log info+ logs to the log file
    ATLAS_LOG_FILE=/var/log/atlas/atlas.log
    ATLAS_LOG_FILE_LEVEL=info
    ATLAS_LOG_FILE_CHMOD=0640

    # original master server api config
    ATLAS_API0_MAX_SERVERS=1000
    ATLAS_API0_MAX_SERVERS_PER_IP=25
    ATLAS_API0_TOKEN_EXPIRY_TIME=24h
    ATLAS_API0_ALLOW_GAME_SERVER_IPV6=false
    ATLAS_API0_MINIMUM_LAUNCHER_VERSION=1.11.3
    ATLAS_API0_REGION_MAP=default
    ATLAS_API0_SERVERLIST_EXPERIMENTAL_DETERMINISTIC_SERVER_ID_SECRET=@api0_server_id_secret
    ATLAS_API0_SERVERLIST_VERIFY_TIME=10s
    ATLAS_API0_SERVERLIST_DEAD_TIME=30s
    ATLAS_API0_SERVERLIST_GHOST_TIME=2m
    ATLAS_API0_STORAGE_ACCOUNTS=sqlite3:/var/lib/atlas/accounts.db
    ATLAS_API0_STORAGE_PDATA=sqlite3:/var/lib/atlas/pdata.db
    ATLAS_API0_MAINMENUPROMOS=file:/etc/atlas/mainmenupromos.json
    ATLAS_API0_MAINMENUPROMOS_UPDATENEEDED=file:/etc/atlas/mainmenupromos_updateneeded.json

    # username lookup
    ATLAS_USERNAMESOURCE=eax-origin

    # origin (the account MUST have app-based two-factor authentication set up)
    ATLAS_ORIGIN_EMAIL=email@example.com
    ATLAS_ORIGIN_PASSWORD=@origin_password
    ATLAS_ORIGIN_TOTP=@origin_totp
    ATLAS_ORIGIN_HAR_GZIP=true
    ATLAS_ORIGIN_HAR_SUCCESS=/var/log/atlas/har
    ATLAS_ORIGIN_HAR_ERROR=/var/log/atlas/har
    ATLAS_ORIGIN_PERSIST=/var/lib/atlas/origin.json

    # eax
    EAX_UPDATE_INTERVAL=24h
    EAX_UPDATE_BUCKET=60

    # metrics
    ATLAS_METRICS_SECRET=@metrics_secret

    # web
    ATLAS_WEB=/usr/share/northstartf/dist

    # ip2location
    ATLAS_IP2LOCATION=/usr/share/ip2location-lite/IP2LOCATION-LITE-DB11.IPV6.BIN

    # misc
    ATLAS_JOKES=true
    ```

    Note: The `@` values in the config are references to systemd credentials, which will be described later.

    ```bash
    sudo nano /etc/atlas/mainmenupromos.json
    ```

    ```json
    {
        "newInfo": {
            "Title1": "%$rui\/bullet_point%`2This text is yellow`0 and this is white!",
            "Title2": "%$rui\/bullet_point%Blah blah blah.",
            "Title3": "%$rui\/bullet_point%Another `2bullet point`0!"
        },
        "largeButton": {
            "Title": "",
            "Text": "",
            "Url": "",
            "ImageIndex": 0
        },
        "smallButton1": {
            "Title": "",
            "Url": "",
            "ImageIndex": 0
        },
        "smallButton2": {
            "Title": "",
            "Url": "",
            "ImageIndex": 0
        }
    }
    ```

    ```bash
    sudo nano /etc/atlas/mainmenupromos_updateneeded.json
    ```

    ```json
    {
        "largeButton": {
            "Title":      "Update Northstar",
            "Text":       "An update is required to play multiplayer.",
            "Url":        "https://github.com/R2Northstar/Northstar/releases/latest",
            "ImageIndex": 12
        }
    }
    ```

6. Create the systemd unit.

    ```bash
    sudo nano /etc/systemd/system/atlas.service
    ```

    ```ini
    [Unit]
    Description=Atlas
    After=network.target

    [Service]
    Type=notify

    User=atlas
    Group=atlas
    ExecStart=/usr/local/bin/atlas /etc/atlas/config

    ConfigurationDirectory=atlas
    LogsDirectory=atlas
    StateDirectory=atlas
    StateDirectoryMode=0750

    AmbientCapabilities=CAP_NET_BIND_SERVICE

    ProtectClock=yes
    ProtectKernelTunables=yes
    ProtectKernelModules=yes
    ProtectKernelLogs=yes
    ProtectControlGroups=yes
    ProtectHome=tmpfs
    ProtectSystem=strict
    PrivateTmp=yes
    PrivateDevices=yes
    PrivateMounts=yes

    LimitNOFILE=32000
    LimitNPROC=32000

    [Install]
    WantedBy=multi-user.target
    ```

7. Set up systemd credentials (doing it this way prevents unprivileged users from reading secrets).

    ```bash
    sudo mkdir /etc/systemd/system/atlas.service.d
    echo "[Service]" | tee /etc/systemd/system/atlas.service.d/credentials.conf
    ```

    For each credential (in the example config: `northstartf.key`, `northstartf.crt`, `api0_server_id_secret`, `metrics_secret`, `origin_password`, `origin_totp`),

    ```bash
    sudo systemd-creds --pretty encrypt --name credential_name - - | tee -a /etc/systemd/system/atlas.service.d/credentials.conf
    # paste the data, then press ctrl+d
    ```

    Note: If you make a mistake or need to change the credentials, you'll need to remove the corresponding lines from `credentials.conf`.

8. Set up logrotate (optional).

    ```bash
    sudo nano /etc/logrotate.d/atlas
    ```

    ```
    /var/log/atlas/atlas.log {
        daily
        missingok
        rotate 900
        dateext
        compress
        compresscmd /usr/bin/zstd
        compressext .zst
        compressoptions --long
        uncompresscmd /usr/bin/unzstd
        delaycompress
        postrotate
            /usr/bin/systemctl kill --signal=SIGHUP atlas.service
        endscript
    }
    ```

    Note: The `postrotate` script is required to tell atlas to re-open log files.

    Note: `delaycompress` is needed to prevent the re-open from being racy.

9. Import data from the old [NorthstarMasterServer](https://github.com/R2Northstar/NorthstarMasterServer) database (optional).

    ```bash
    sudo mkdir /var/lib/atlas
    sudo atlas-import --progress /path/to/old/playerdata.db /var/lib/atlas/accounts.db /var/lib/atlas/pdata.db
    ```

10. Start atlas.

    ```bash
    sudo systemctl daemon-reload
    sudo systemctl enable --now atlas.service
    ```

## Maintenance

  - **Get service status**

    ```bash
    sudo systemctl status atlas
    ```

  - **Restart**

    ```bash
    sudo systemctl restart atlas
    ```

  - **Reload files** (logs, ip2location db, etc)

    ```bash
    sudo systemctl kill --signal=SIGHUP atlas
    ```

  - **Backup databases**

    ```bash
    sudo -g atlas sqlite3 /var/lib/atlas/accounts.db 'VACUUM INTO "/path/to/backup/accounts.db"'
    sudo -g atlas sqlite3 /var/lib/atlas/pdata.db 'VACUUM INTO "/path/to/backup/pdata.db"'
    ```

    This safely creates a consistent backup of a running Atlas instance.

  - **View error logs**

    ```bash
    sudo journalctl -u atlas
    ```

  - **View systemd credentials**

    ```bash
    sudo CREDENTIALS_DIRECTORY=/run/credentials/atlas.service systemd-creds list
    ```

  - **Query database**

    ```bash
    sudo -g atlas sqlite3 -readonly /var/lib/atlas/accounts.db
    sudo -g atlas sqlite3 -readonly /var/lib/atlas/pdata.db
    ```

  - **Query database (read-write)**

    ```bash
    sudo -u atlas sqlite3 /var/lib/atlas/accounts.db
    sudo -u atlas sqlite3 /var/lib/atlas/pdata.db
    ```

  - **Profile memory/cpu usage**

    1. Enable the debug server by adding `INSECURE_DEBUG_SERVER_ADDR=127.0.0.1:8080` to the config and restarting Atlas.
    2. If necessary, forward the port locally over SSH.
    3. Use `go tool pprof` to view the profile, for example:
       - `go tool pprof -http : http://127.0.0.1:8080/debug/pprof/heap`
       - `go tool pprof -http : http://127.0.0.1:8080/debug/pprof/profile?seconds=30`

  - **Debug a failed Origin login**

    1. Get the latest HAR archive from `/var/log/atlas/har`.
    2. Uncompress it.
    3. Open it in a viewer like [this one](http://www.softwareishard.com/har/viewer/). Note that the network tab in browser devtools often doesn't work since they aren't fully spec-compliant.

    You can reset the Origin auth state by removing `/var/lib/atlas/origin.json` and restarting Atlas.

## Monitoring

Sample promscrape config:

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'atlas'
    static_configs:
      - targets:
        - 'northstar.tf:443'
    scheme: https
    relabel_configs:
      - # enable internal metrics
        target_label: '__param_secret'
        replacement: 'YOUR_METRICS_SECRET'
      - # enable geohash-based metrics
        target_label: '__param_geo'
        replacement: 'true'
      - # prettify the instance name
        source_labels: ['__address__']
        target_label: 'instance'
        regex: '^(?:.+//)([^/:]+)'
        replacement: '${1}'
```

Note: VictoriaMetrics generally performs better than Prometheus.

<!-- TODO: sample dashboards and JSON. -->

## Automatic database backups

You can use [Litestream](https://litestream.io/) for real-time database backups.

### Installation

1. Install Litestream.

    ```bash
    wget -qO- https://github.com/benbjohnson/litestream/releases/download/v0.3.9/litestream-v0.3.9-linux-amd64-static.tar.gz | sudo tar xzvf - --no-same-owner -C /usr/local/bin litestream
    ```

2. Create a template systemd service and configuration directory.

    ```bash
    # note: we restrict the permissions since the configs usually contain credentials
    sudo mkdir --mode=0700 /etc/litestream
    sudo nano /etc/systemd/system/litestream@.service
    ```

    ```ini
    [Unit]
    Description=Litestream (%I)

    [Service]
    User=root
    Group=root

    Restart=always
    ExecStart=/usr/local/bin/litestream replicate -config /etc/litestream/%I.yml

    ProtectClock=yes
    ProtectKernelTunables=yes
    ProtectKernelModules=yes
    ProtectKernelLogs=yes
    ProtectControlGroups=yes
    PrivateTmp=yes
    PrivateDevices=yes
    PrivateMounts=yes

    [Install]
    WantedBy=multi-user.target
    ```

3. Create the configuration for Atlas.

    ```bash
    sudo nano /etc/litestream/atlas.yml
    ```

    ```yml
    dbs:
    - path: /var/lib/atlas/accounts.db
      replicas:
      - url: sftp://USER:PASSWORD@SERVER:PORT/litestream/accounts
        retention: 336h # 14d
        snapshot-interval: 24h
        sync-interval: 300s
    - path: /var/lib/atlas/pdata.db
      replicas:
      - url: sftp://USER:PASSWORD@SERVER:PORT/litestream/pdata
        retention: 336h # 14d
        snapshot-interval: 24h
        sync-interval: 300s
    ```

    Note: Update the database paths as required. You can also use other kinds of replicas or change the times as described [here](https://litestream.io/reference/config/).

    Note: You will need enough free disk space to store `snapshot-interval` worth of database changes locally.

4. Start litestream.

    ```bash
    sudo systemctl enable --now litestream@atlas.service
    ```

### Recovery

1. Stop litestream (it will break if the database is recreated while it is running) and atlas.

    ```bash
    sudo systemctl stop litestream@atlas atlas
    ```

2. Remove the old database.

    ```bash
    sudo rm -rfv /var/lib/atlas/{,.}{accounts,pdata}.db{,-wal,-shm,-litestream}
    ```

    Note: Change the path as required.

3. Restore the database.

    ```bash
    sudo litestream restore -v -config /etc/litestream/atlas.yml /var/lib/atlas/accounts.db
    sudo litestream restore -v -config /etc/litestream/atlas.yml /var/lib/atlas/pdata.db
    ```

    Note: See [here](https://litestream.io/reference/restore/) for additional options like restoring old versions.

    Note: You can also run these commands separately and use the `-o` flag to restore to a new copy of the database.

4. Start litestream and atlas.

    ```bash
    sudo systemctl start litestream@atlas atlas
    ```

### Maintenance

- **View backup status**

  ```bash
  sudo litestream generations -config /etc/litestream/atlas.yml /var/lib/atlas/accounts.db
  sudo litestream generations -config /etc/litestream/atlas.yml /var/lib/atlas/pdata.db
  sudo litestream wal -config /etc/litestream/atlas.yml /var/lib/atlas/accounts.db
  sudo litestream wal -config /etc/litestream/atlas.yml /var/lib/atlas/pdata.db
  ```

- **Download an old version of a database**

  ```bash
  sudo litestream restore -v -config /etc/litestream/atlas.yml -timestamp 2022-12-12T06:00:00Z -o pdata-old.db /var/lib/atlas/pdata.db
  ```
