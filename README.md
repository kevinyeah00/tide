# Tide

A distributed runtime for monolithic applications that enables the application resource space to dynamically and adaptively change in real time like tides with the rotation of time.

## Compile

```bash
go build -o bin/tide cmd/tide/main.go
```

## Usage

Currently, starting Tide requires sudo privileges for two reasons:
1. The current implementation uses the Linux message queue as the medium of interaction between Tide and the application. In order to break through the number of messages in the queue and the size of a single message set by Linux to the message queue by default, rlimit is set by a system call during the initial startup of the program.
2. Tide needs to create and manage cgroups.

Execute the following command:

```bash
sudo bin/tide --help
```

The following output is obtained:

```bash
Tide is a distributed computing framework

Usage:
tide [flags]
tide [command]

Available Commands:
help        Help about any command
serve       Start tide serve process
submit      submit job to tide system
```
Where `tide serve` is the command to start the Tide system, and tide submit is the command to submit tasks to the Tide system.

### `tide serve` command

The parameters for the tide serve command are as follows:

- addresses
    - Addresses of other nodes in the cluster where Tide can be accessed (for example, `10.10.0.154:10001`), multiple addresses are separated by English commas
- cpu-num
    - The number of CPU cores that the Tide can use, not valid if the total number of cores is exceeded.
- name
    - Node name, uniquely identifies the device, null generates a random string.
- outbound-ip
    - outbound-ip, if empty, a random string will be generated.
- port
    - Port on which Tide ApiServer listens
- role
    - The role of the device in the cluster, there are three categories: things, edge, and cloud.

- Runtime (code internal configuration): you need to adjust the runtime estimate under `pkg/dev` based on the runtime of the load under certain resource constraints to achieve better results.

### Example

There are three devices with the following configurations and roles in the table below:

| Alias | IP          | Port  | Role   |
| --    | --          |  --   | --     |
| A     | 10.10.0.151 | 10001 | things |
| B     | 10.10.0.154 | 10001 | edge   |
| C     | 10.208.8.2  | 10001 | cloud  |

startup of A device
```bash
sudo bin/tide serve -name A -port 10001 -role things
```

startup of B device
```bash
sudo bin/tide serve -name B -port 10001 -role edge -addresses 10.10.0.151:10001
```

startup of C device
```bash
sudo bin/tide serve -name C -port 10001 -role cloud -addresses 10.10.0.151:10001,10.208.8.2:10001
```

In the above device, after the sequential execution, you can complete the construction of the three-device cluster

### `tide submit` command

Tide submit supports submitting tasks to any things node with the following parameters:

- server
    - The address of Tide on the target node (e.g. `10.10.0.154:10001`, or `127.0.0.1:10001` for local submission).
- image
    - Image name
- command
    - Task startup command in the container, such as `"python3 /app/main.py"` (the command needs to be in double quotes to prevent some of the parameters in the startup command from being parsed by tide submit)
- name
    - Name of the application, uniquely identifies the application, if empty, a random string will be generated.
- volume
    - Mounted volume, in the format `hostPath:containerPath`, e.g. `/xxx/zzz:/data`.


### Cgroup downgrade (Ubuntu 22.04 LTS for example)

> Reference: https://support.huaweicloud.com/intl/zh-cn/ucs_faq/ucs_faq_0032.html

Tide currently uses Cgroup version v1 in its code, while some systems use v2 by default, so it needs to be downgraded.

``bash
sudo nano /etc/default/grub
``

- Locate `GRUB_CMDLINE_LINUX`.

- Add or modify the `systemd.unified_cgroup_hierarchy` value, a value of 1 is cgroup v2, a value of 0 is cgroup v1

``bash
sudo grub-mkconfig -o /boot/grub/grub.cfg

reboot # Make sure other users work before rebooting
```
