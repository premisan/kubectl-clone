# kubectl-clone

[English](README.md) | [한국어](README.ko.md)

[![Go Report Card](https://goreportcard.com/badge/github.com/premisan/kubectl-clone)](https://goreportcard.com/report/github.com/premisan/kubectl-clone)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

`kubectl-clone` is a powerful, unified cloning and migration engine for Kubernetes. It combines object manifest cloning, incremental pod file synchronization, and cross-cluster volume data migration into a single, intuitive `kubectl` plugin.

```text
kubectl clone
├── resource (r)  # Clone k8s objects (Secrets, ConfigMaps, Deployments, CRDs) with auto-sanitization
├── file     (f)  # Incremental and recursive file/directory sync between Pod and Local or Pod and Pod
├── pvc      (p)  # Cross-Cluster & Cross-Namespace PVC data migration without public IPs or LoadBalancers
├── ls       (l)  # List files & permissions inside containers (supports Distroless pods via debug container)
└── tree     (t)  # Display hierarchical directory tree inside containers (supports Distroless pods)
```

---

## Architecture: Dual-Exec Stdio Bridge

For cross-cluster or cross-namespace PVC migrations, `kubectl-clone` eliminates the need for public IPs, Ingress, NodePorts, or cloud LoadBalancers. It uses the local CLI to establish a zero-copy, bi-directional memory pipe between two clusters over secure Kubernetes API HTTPS connections.

```text
+-------------------------------------------------------------------+
|                  Local Machine (kubectl clone pvc)                |
|                                                                   |
|   +-- [Cluster A Exec Stream] ----- [Cluster B Exec Stream] --+   |
|   |   Stdout  ---------------------------> Stdin              |   |
|   |   Stdin   <--------------------------- Stdout             |   |
|   +-----------------------------------------------------------+   |
+-------------------|-------------------------------|---------------+
                    | (k8s API HTTPS)               | (k8s API HTTPS)
                    v                               v
    +-------------------------------+   +-------------------------------+
    | Cluster A (Source Pod)        |   | Cluster B (Target Pod)        |
    | PVC: source-pvc mounted       |   | PVC: dest-pvc mounted         |
    | `rsync --server --sender ...` |   | `rsync --server ...`          |
    +-------------------------------+   +-------------------------------+
```

---

## Installation

### From Source (Recommended)

```bash
git clone https://github.com/premisan/kubectl-clone.git
cd kubectl-clone
make install
```

By default, this installs `kubectl-clone` and the completion binary `kubectl_complete-clone` into `~/.local/bin` (or custom path via `make install INSTALL_PATH=/usr/local/bin`).

### Using `go install`

```bash
go install github.com/premisan/kubectl-clone/cmd/kubectl-clone@latest
```

---

## Shell Autocompletion

`kubectl-clone` fully implements the Kubernetes Plugin Completion Specification ([KEP-2580](https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/2580-kubectl-plugin-completion)).

If you already have `kubectl` shell completion enabled in your environment (`source <(kubectl completion zsh)`), **Tab completion works automatically out-of-the-box!**

To explicitly enable shell completion:

#### Zsh
```bash
# Add to ~/.zshrc:
source <(kubectl clone completion zsh)
```

#### Bash
```bash
# Add to ~/.bashrc:
source <(kubectl clone completion bash)
```

#### Fish
```bash
kubectl clone completion fish | source
```

---

## Command Reference & Practical Examples

### 1. Kubernetes Resource Object Cloning (`resource` / `r`)

Clones any Kubernetes resource manifest across namespaces or clusters while automatically cleaning up server-managed metadata (`uid`, `resourceVersion`, `managedFields`), runtime `status`, and dynamic spec fields (`clusterIP`, `volumeName`).

```bash
# Clone a secret to 'prod' namespace (keeps original name)
kubectl clone resource secret db-credentials --to-namespace prod

# Using shorthand 'r' to clone from 'kube-system' into the active context's default namespace
kubectl clone r cm coredns -n kube-system

# Clone a deployment with a new name in the same namespace
kubectl clone r deployment web-app --to-name web-app-v2

# Clone a cluster-scoped ClusterRole with a new name
kubectl clone r clusterrole admin-template --to-name custom-admin

# Clone across different clusters (contexts)
kubectl clone r configmap app-config \
  --context staging-cluster \
  --to-context prod-cluster \
  --to-namespace production

# Overwrite destination resource if it already exists
kubectl clone r secret db-credentials --to-namespace prod --overwrite

# Client-side dry-run and output sanitized YAML to stdout
kubectl clone r service backend-svc --to-namespace dev --dry-run=client -o yaml

# Server-side validation against target cluster
kubectl clone r deployment worker --to-namespace prod --dry-run=server
```

#### Automatic Metadata Sanitization Engine

| Category | Purged / Sanitized Fields |
| :--- | :--- |
| **Server Metadata** | `uid`, `resourceVersion`, `generation`, `creationTimestamp`, `managedFields`, `ownerReferences`, `selfLink`, `generateName` |
| **Status** | Entire runtime `status` block is removed |
| **Annotations** | `kubectl.kubernetes.io/last-applied-configuration`, deployment revisions, PV controller bindings |
| **Service** | `spec.clusterIP` (unless "None"), `spec.clusterIPs`, `spec.healthCheckNodePort`, and `nodePort` |
| **PersistentVolumeClaim** | `spec.volumeName` (allows fresh dynamic PV binding) |
| **ServiceAccount** | Legacy auto-generated token secret references |

---

### 2. Pod File & Directory Syncing (`file` / `f`)

Replaces standard `kubectl cp` with a full-fledged **Rsync wire-protocol engine** running directly over Kubernetes SPDY/WebSocket streams.
- **Actively leverages Ephemeral Debug Containers (`--use-debug=true`, enabled by default)**: Seamlessly copies files and volumes from **Distroless and Scratch pods** (such as `coredns`, `etcd`, minimal Go/Rust images) with zero existing utilities.
- Accesses target container filesystems and mounted volumes transparently via `/proc/<PID>/root`.
- **Incremental diff transfer**: Only transfers changed file chunks (Delta-transfer).
- Supports **Pod <-> Local** and **Cross-Cluster Pod <-> Pod** file synchronization.

```bash
# 1. Pod -> Local: Seamlessly copy files/volumes from Distroless pods (e.g. Corefile from coredns)
kubectl clone file coredns-xxxx:/etc/coredns/Corefile ./Corefile

# 2. Local -> Pod: Sync local build assets into a pod (incremental diff only)
kubectl clone f ./dist/ my-pod:/usr/share/nginx/html/

# 3. Mirroring: Sync with --delete (removes extraneous files on destination)
kubectl clone f ./static/ my-pod:/app/static/ --delete

# 4. Multi-container pod: Target a specific container
kubectl clone f -c app-container my-pod:/data/backup.tar.gz ./backup.tar.gz

# 5. Pod -> Pod: Sync data between pods in different namespaces or clusters
kubectl clone file \
  --src-context cluster-dev --src-namespace dev pod-a:/app/data/ \
  --dest-context cluster-prod --dest-namespace prod pod-b:/app/data/

# 6. Trial run (Dry-Run): Preview files that would be transferred
kubectl clone f ./assets/ web-pod:/var/www/ --dry-run
```

---

### 3. PersistentVolumeClaim Volume Migration (`pvc` / `p`)

Migrate, copy, and mirror volume contents between PVCs without intermediate local disk storage.

```bash
# 1. Same Namespace: Automatically selects 'direct' strategy (local I/O within a single worker pod)
kubectl clone pvc source-pvc dest-pvc

# 2. Cross-Namespace: Sync between 'dev' and 'prod' namespaces
kubectl clone p source-pvc dest-pvc \
  --src-namespace dev \
  --dest-namespace prod

# 3. ⭐️ Cross-Cluster Migration: Migrate PVC data across clouds/clusters without public IP
kubectl clone pvc source-pvc dest-pvc \
  --src-context eks-cluster-staging --src-namespace dev \
  --dest-context gke-cluster-prod --dest-namespace prod \
  --strategy stdio-bridge \
  --delete -z

# 4. Sync a specific subfolder inside the PVC
kubectl clone p source-pvc dest-pvc \
  --src-path /uploads/2026/ \
  --dest-path /archive/2026/

# 5. Mirroring: Delete files in destination that do not exist in source
kubectl clone p source-pvc dest-pvc --delete

# 6. Custom container image for migration worker pods
kubectl clone p source-pvc dest-pvc --image bitnami/rsync:latest
```

#### Replication Strategies

- `auto` (default): Automatically uses `direct` if source & destination are in the same cluster/namespace; otherwise uses `stdio-bridge`.
- `direct`: Mounts both PVCs into a single worker pod for local disk-to-disk speed.
- `stdio-bridge`: Streams data bi-directionally between two clusters using the local CLI as an in-memory cross-pipe.

#### 💡 Live Workload Support & Near Zero-Downtime 2-Phase Migration

`kubectl clone pvc` automatically detects running pods actively mounting the source RWO PVC and **co-locates the migration worker on the exact same node** with `ReadOnly` volume mounting. This completely bypasses Kubernetes `Multi-Attach error` collisions without interrupting the active workload.

**Recommended 2-Phase Workflow:**
1. **Pre-sync (Live / Zero Downtime)**: Run `kubectl clone pvc src-pvc dest-pvc` while the application is online to replicate the bulk of data (GBs/TBs) in the background.
2. **Final Cutover (Seconds of downtime)**: Scale down the workload (`kubectl scale --replicas=0`), run `kubectl clone pvc src-pvc dest-pvc` again to sync only the delta in seconds, then start the new deployment!

---

### 4. Container File & Directory Inspection (`ls` / `tree`)

Explore and browse files, permissions, and directory hierarchies inside pods before cloning. Actively utilizes Ephemeral Debug Containers to inspect **Distroless/Scratch pods without native `ls`, `sh`, or `tree` utilities**, supporting standard Linux command flags.

#### `ls` / `l` Examples
```bash
# 1. Standard long listing of pod root directory
kubectl clone ls my-pod

# 2. Inspect Distroless coredns pod via shorthand 'l' (shows hidden files)
kubectl clone l coredns-xxxx:/etc/coredns -n kube-system -a

# 3. Sort files by modification time, newest first (-t)
kubectl clone ls my-pod:/var/log -t

# 4. Sort by file size, largest first (-S) in reverse order (-r)
kubectl clone ls my-pod:/data -S -r

# 5. Recursive directory listing (-R)
kubectl clone ls my-pod:/etc -R

# 6. Single-column output (-1) without detailed permissions
kubectl clone ls my-pod:/app -l=false -1
```

#### `tree` / `t` Examples
```bash
# 1. View directory hierarchy in a tree view with depth limit (-L)
kubectl clone tree coredns-xxxx:/etc -n kube-system -L 2

# 2. View directories only via shorthand 't' (-d)
kubectl clone t my-pod:/app -d

# 3. Show file permissions and human-readable sizes (-p, -h)
kubectl clone tree my-pod:/var/log -p -h

# 4. Display all files including hidden ones, excluding specific patterns (-I)
kubectl clone tree my-pod:/app -a -I "*.log|node_modules|*.tmp"

# 5. Print full absolute paths for each entry (--full-path)
kubectl clone tree my-pod:/etc/kubernetes/pki --full-path
```

---

## Global & Subcommand Flags

### `kubectl clone resource` / `r`
```text
      --to-namespace string      Destination namespace (defaults to active context namespace)
      --to-context string        Destination kubeconfig context
      --to-kubeconfig string     Path to destination kubeconfig file
      --to-name string           New name for the cloned resource
      --overwrite                Overwrite target resource if it already exists
      --dry-run string           Dry-run strategy: 'none', 'client', or 'server'
  -o, --output string            Output format: 'yaml' or 'json'
      --clear-nodeport           Clear Service nodePorts to prevent port collision (default true)
      --preserve-pvc-volume      Preserve spec.volumeName on PVCs (default false)
```

### `kubectl clone file` / `f`
```text
  -c, --container string        Target container name inside pod
      --src-context string      Source kubeconfig context
  -n, --src-namespace string    Source namespace
      --dest-context string     Destination kubeconfig context
      --dest-namespace string   Destination namespace
      --delete                  Delete extraneous files from destination
  -z, --compress                Compress data stream during transfer (default true)
      --exclude strings         Exclude files matching PATTERN (can be specified multiple times)
      --include strings         Don't exclude files matching PATTERN (can be specified multiple times)
      --bwlimit string          Bandwidth limit (e.g. '10m', '5000')
      --checksum                Skip based on checksum, not mod-time & size
      --partial                 Keep partially transferred files
      --rsync-args string       Pass arbitrary raw arguments directly to rsync
      --use-debug               Use ephemeral debug container to support Distroless pods (default true)
      --debug-image string      Container image for ephemeral debug containers (default "busybox:latest")
      --dry-run                 Perform a trial run with no changes made
```

### `kubectl clone pvc` / `p`
```text
      --src-context string      Source kubeconfig context
  -n, --src-namespace string    Source namespace
      --dest-context string     Destination kubeconfig context
      --dest-namespace string   Destination namespace
      --src-path string         Subpath in source PVC (default "/")
      --dest-path string        Subpath in destination PVC (default "/")
      --strategy string         Replication strategy: 'auto', 'direct', or 'stdio-bridge' (default "auto")
      --delete                  Delete extraneous files in destination PVC
  -z, --compress                Compress data stream during transfer (default true)
      --exclude strings         Exclude files matching PATTERN (can be specified multiple times)
      --include strings         Don't exclude files matching PATTERN (can be specified multiple times)
      --bwlimit string          Bandwidth limit (e.g. '10m', '5000')
      --checksum                Skip based on checksum, not mod-time & size
      --partial                 Keep partially transferred files
      --rsync-args string       Pass arbitrary raw arguments directly to rsync
      --image string            Container image for migration pods (default "alpine:latest")
      --dry-run                 Perform a trial run with no changes made
```

### `kubectl clone ls` / `l`
```text
  -a, --all                     Do not ignore entries starting with . (includes . and ..)
  -A, --almost-all              Do not list implied . and ..
  -l, --long                    Use a long listing format (default true)
      --human-readable          Print human readable sizes like 1K, 234M, 2G (default true)
  -t, --sort-time               Sort by modification time, newest first
  -S, --sort-size               Sort by file size, largest first
  -r, --reverse                 Reverse order while sorting
  -R, --recursive               List subdirectories recursively
  -1, --one                     List one file per line
  -c, --container string        Target container name inside pod
  -n, --namespace string        Namespace scope for this request
      --context string          The kubeconfig context to use
      --use-debug               Use ephemeral debug container for Distroless pods (default true)
      --debug-image string      Container image for ephemeral debug container (default "busybox:latest")
```

### `kubectl clone tree` / `t`
```text
  -L, --level int               Max display depth of the directory tree
  -d, --dirs-only               List directories only
  -a, --all                     All files are listed (including hidden files)
  -p, --permissions             Print the file type and permissions flags
  -s, --size                    Print the size in bytes of each file
  -h, --human-readable          Print the size in human readable format (e.g. 1K, 234M)
  -I, --ignore string           Do not list files that match given pattern (e.g. "*.log|*.tmp")
      --full-path               Print the full path prefix for each file
  -c, --container string        Target container name inside pod
  -n, --namespace string        Namespace scope for this request
      --context string          The kubeconfig context to use
      --use-debug               Use ephemeral debug container for Distroless pods (default true)
      --debug-image string      Container image for ephemeral debug container (default "busybox:latest")
```

---

## Uninstallation

To remove `kubectl-clone` from your system, use one of the following methods:

### Using Makefile
If you installed from source via `make install`:
```bash
make uninstall

# If installed with a custom INSTALL_PATH:
make uninstall INSTALL_PATH=/usr/local/bin
```

### Manual Removal
Delete the binary and completion symlink from your installation directory:
```bash
# Default installation path (~/.local/bin)
rm -f ~/.local/bin/kubectl-clone ~/.local/bin/kubectl_complete-clone

# Custom installation path (e.g. /usr/local/bin)
sudo rm -f /usr/local/bin/kubectl-clone /usr/local/bin/kubectl_complete-clone

# If installed via go install
rm -f $(go env GOPATH)/bin/kubectl-clone
```

### Shell Autocompletion Cleanup (Optional)
If you configured shell autocompletion in your shell configuration profile (e.g. `~/.zshrc`, `~/.bashrc`), remove the corresponding line:
```bash
# Remove this line from ~/.zshrc or ~/.bashrc:
source <(kubectl clone completion <shell>)
```

To verify the uninstallation, run:
```bash
kubectl plugin list
```

---

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.
