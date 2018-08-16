ftpd
=========

**ftpd** is a FTP server implementation in Go.
It does support basic user authentication and access control.

## Configuration
Basic configuration like listening address is done using the command-line interface, while advanced user configuration has to be done using YAML files. An example listing can be found below.

```bash
echo > users.yaml <<EOF
users:
  anonymous:
    home: /tmp/anonymous
    hash: ""
    password: ""
    group: anonymous
  espe:
    home: /home/admin
    password: "example-password"
    group: admin
groups:
  admin:
    create:
    - file
    - dir
    handle:
    - file
    - dir
    delete:
    - file
    - dir
  anonymous:
    create: []
    handle:
    - file
    - dir
    delete: []
EOF
ftpd
```

