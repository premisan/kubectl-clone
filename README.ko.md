# kubectl-clone

[English](README.md) | [한국어](README.ko.md)

[![Go Report Card](https://goreportcard.com/badge/github.com/premisan/kubectl-clone)](https://goreportcard.com/report/github.com/premisan/kubectl-clone)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

`kubectl-clone`은 쿠버네티스 리소스 객체(YAML), 파드 내부 파일/디렉토리, PVC 볼륨 데이터를 네임스페이스와 클러스터 경계를 넘어 자유롭게 복제하고 동기화할 수 있는 고성능 통합 CLI 도구입니다.

```text
kubectl clone
├── resource (r)  # k8s 리소스 오브젝트(Secret, ConfigMap, Deployment, CRD 등) 자동 정제 복제
├── file     (f)  # 파드 <-> 로컬, 파드 <-> 파드 증분 및 재귀 파일 동기화 (alias: cp, sync)
└── pvc      (p)  # 공인 IP/LB 없는 크로스 클러스터 PVC 볼륨 마이그레이션 (alias: volume, pv)
```

---

## 핵심 아키텍처: Dual-Exec Stdio Bridge

네임스페이스 간 또는 완전히 분리된 다른 클러스터(EKS ➔ GKE 등) 간에 PVC를 복제할 때, **공인 IP, 인그레스(Ingress), 로드밸런서(LoadBalancer) 생성이 전혀 필요 없습니다.**

로컬 머신의 `kubectl-clone` 프로세스가 두 클러스터의 API 서버로 SPDY/WebSocket 보안 터널을 연결하여, 메모리 상에서 양방향 파이프(`io.Pipe`)로 rsync 와이어 프로토콜 입출력을 직접 무복사(Zero-copy) 스트리밍 중계합니다.

```text
+-------------------------------------------------------------------+
|                  로컬 머신 (kubectl clone pvc)                    |
|                                                                   |
|   +-- [Cluster A Exec Stream] ----- [Cluster B Exec Stream] --+   |
|   |   Stdout  ---------------------------> Stdin              |   |
|   |   Stdin   <--------------------------- Stdout             |   |
|   +-----------------------------------------------------------+   |
+-------------------|-------------------------------|---------------+
                    | (k8s API HTTPS)               | (k8s API HTTPS)
                    v                               v
    +-------------------------------+   +-------------------------------+
    | Cluster A (소스 파드)          |   | Cluster B (타겟 파드)          |
    | PVC: source-pvc 마운트        |   | PVC: dest-pvc 마운트          |
    | `rsync --server --sender ...` |   | `rsync --server ...`          |
    +-------------------------------+   +-------------------------------+
```

- **클라우드 비용 0원**: 외부 노출용 LB나 포트포워딩이 불필요합니다.
- **로컬 디스크 소모 0**: 중간에 로컬 디스크에 임시 파일을 쓰지 않고 실시간 커널 메모리 파이프로 전송합니다.
- **전송 최적화**: rsync의 증분(Delta) 전송, 체크섬 무결성 검증, 데이터 압축(`-z`)을 그대로 활용합니다.

---

## 설치 방법

### 소스코드에서 빌드 및 설치 (권장)

```bash
git clone https://github.com/premisan/kubectl-clone.git
cd kubectl-clone
make install
```

기본적으로 `~/.local/bin`에 `kubectl-clone` 및 플러그인 자동 완성 심볼릭 링크인 `kubectl_complete-clone`이 함께 설치됩니다. (전역 경로 설치 시 `sudo make install INSTALL_PATH=/usr/local/bin` 실행)

### `go install`을 통한 설치

```bash
go install github.com/premisan/kubectl-clone/cmd/kubectl-clone@latest
```

---

## 쉘 자동 완성 (Shell Autocompletion)

`kubectl-clone`은 쿠버네티스 공식 플러그인 완성 규약([KEP-2580](https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/2580-kubectl-plugin-completion))을 완벽히 지원합니다.

이미 쉘에 `kubectl` 자동 완성(`source <(kubectl completion zsh)`)이 설정되어 있다면, **설치 즉시 별도 설정 없이 Tab 키 자동 완성이 동작합니다.**

수동으로 자동 완성을 등록하고자 하는 경우:

#### Zsh (기본 Mac 쉘)
```bash
# ~/.zshrc 파일에 추가:
source <(kubectl clone completion zsh)
```

#### Bash
```bash
# ~/.bashrc 또는 ~/.bash_profile에 추가:
source <(kubectl clone completion bash)
```

#### Fish
```bash
kubectl clone completion fish | source
```

---

## 서브커맨드별 상세 가이드 및 실무 예제

### 1. 쿠버네티스 리소스 객체 복제 (`resource` / `r`)

쿠버네티스 API 리소스를 다른 네임스페이스나 다른 클러스터로 복제합니다. 서버가 자동으로 부여하는 내부 메타데이터(`uid`, `resourceVersion`, `managedFields` 등)와 런타임 `status`, 포트 충돌 요소(Service `clusterIP`)를 깔끔하게 정제하여 충돌 없이 생성합니다.

```bash
# 1. 다른 네임스페이스로 Secret 복제 (이름 유지)
kubectl clone resource secret db-credentials --to-namespace prod

# 2. 한 글자 단축어(r)를 사용해 'kube-system'의 ConfigMap을 현재 기본 네임스페이스로 복제
kubectl clone r cm coredns -n kube-system

# 3. 동일 네임스페이스 내에서 새로운 이름으로 Deployment 복제
kubectl clone r deployment web-api --to-name web-api-v2

# 4. 클러스터 스코프 리소스(ClusterRole, StorageClass 등)를 새 이름으로 복제
kubectl clone r clusterrole view-template --to-name custom-view-role

# 5. 서로 다른 클러스터 컨텍스트 간 리소스 복제
kubectl clone r configmap app-config \
  --context staging-cluster \
  --to-context prod-cluster \
  --to-namespace production

# 6. 대상 위치에 이미 리소스가 존재할 때 강제로 덮어쓰기 (Update/Replace)
kubectl clone r secret db-credentials --to-namespace prod --overwrite

# 7. 클라이언트 드라이런으로 실제 생성 없이 정제된 YAML 매니페스트 확인
kubectl clone r service backend-svc --to-namespace dev --dry-run=client -o yaml

# 8. 대상 클러스터 API 서버를 통한 사전 유효성 검증 (Server-side dry-run)
kubectl clone r deployment worker --to-namespace prod --dry-run=server
```

#### 자동 리소스 정제 엔진 (Sanitization)
| 대상 분류 | 자동 제거 및 정제되는 필드 |
| :--- | :--- |
| **서버 관리 메타데이터** | `uid`, `resourceVersion`, `generation`, `creationTimestamp`, `managedFields`, `ownerReferences`, `selfLink`, `generateName` |
| **런타임 상태값** | `status` 필드 전체 삭제 |
| **시스템 어노테이션** | `kubectl.kubernetes.io/last-applied-configuration`, 디플로이먼트 리비전, PV 바인딩 관련 어노테이션 |
| **Service (`v1/Service`)** | `spec.clusterIP` (None 제외), `spec.clusterIPs`, `spec.healthCheckNodePort`, `spec.ports[*].nodePort` |
| **PersistentVolumeClaim** | `spec.volumeName` (새 네임스페이스에서 신규 PV와 바인딩될 수 있도록 초기화) |
| **ServiceAccount** | 자동 생성된 레거시 토큰 시크릿 참조(`secrets`) 목록 제거 |

---

### 2. 파드 파일/디렉토리 고속 동기화 (`file` / `f`)

기존 `kubectl cp`의 느린 속도와 권한 문제를 해결하기 위해, 파드의 SPDY Exec 스트림 위에서 **Linux Rsync 엔진**을 가동합니다.
- **디버그 컨테이너 적극 활용 (`--use-debug=true`, 기본 활성화)**: `coredns`, `etcd`, Distroless, Scratch 등 `sh`, `ls`, `tar`가 전무한 파드라도 Ephemeral 디버그 컨테이너를 주입하여 프로세스 네임스페이스(`/proc/<PID>/root`)를 통해 **모든 파일과 마운트된 볼륨에 100% 정상 접근**합니다.
- **증분(Delta) 전송**: 변경된 파일 블록만 diff 전송
- **Pod <-> 로컬** 및 **크로스 클러스터 Pod <-> Pod** 양방향 파일 동기화 완벽 지원

```bash
# 1. 파드 -> 로컬 복제: Distroless 파드(예: coredns의 Corefile)에서도 문제없이 즉시 다운로드
kubectl clone file coredns-xxxx:/etc/coredns/Corefile ./Corefile

# 2. 로컬 -> 파드 복제: 빌드 결과물(dist 폴더)을 파드 내부로 증분 전송
kubectl clone f ./dist/ my-pod:/usr/share/nginx/html/

# 3. 미러링 (--delete): 원본에 없는 대상 파드의 불필요한 파일 자동 정리
kubectl clone f ./static/ my-pod:/app/static/ --delete

# 4. 멀티 컨테이너 파드: 특정 컨테이너 지정 (-c)
kubectl clone f -c app-container my-pod:/data/backup.tar.gz ./backup.tar.gz

# 5. 파드 -> 파드 복제: 서로 다른 네임스페이스 또는 클러스터 간 파드 파일 직접 전송
kubectl clone file \
  --src-context cluster-dev --src-namespace dev pod-a:/app/data/ \
  --dest-context cluster-prod --dest-namespace prod pod-b:/app/data/

# 6. 드라이런 (--dry-run): 실제 전송 전 변경될 파일 목록 미리보기
kubectl clone f ./assets/ web-pod:/var/www/ --dry-run
```

---

### 3. PersistentVolumeClaim 볼륨 데이터 복제 (`pvc` / `p`)

스토리지 클래스나 클라우드가 다른 PVC 간에 볼륨 데이터를 안전하게 마이그레이션합니다.

```bash
# 1. 동일 네임스페이스 내 초고속 복제 (Direct 로컬 I/O 전략 자동 선택)
kubectl clone pvc source-pvc dest-pvc

# 2. 다른 네임스페이스 간 PVC 데이터 동기화
kubectl clone p source-pvc dest-pvc \
  --src-namespace dev \
  --dest-namespace prod

# 3. ⭐️ 다른 클러스터 간 PVC 마이그레이션 (공인 IP/LoadBalancer 없이 복제!)
kubectl clone pvc source-pvc dest-pvc \
  --src-context eks-staging --src-namespace dev \
  --dest-context gke-prod --dest-namespace prod \
  --strategy stdio-bridge \
  --delete -z

# 4. PVC 내부의 특정 하위 경로만 선택하여 동기화
kubectl clone p source-pvc dest-pvc \
  --src-path /uploads/2026/ \
  --dest-path /archive/2026/

# 5. 완전 미러링 (--delete): 소스 PVC에 없는 타겟 PVC 파일 삭제
kubectl clone p source-pvc dest-pvc --delete

# 6. 임시 마이그레이션 워커 파드에 사용할 커스텀 컨테이너 이미지 지정
kubectl clone p source-pvc dest-pvc --image bitnami/rsync:latest
```

#### 복제 전략 (Replication Strategies)
- `auto` (기본값): 소스와 대상이 동일 클러스터/동일 네임스페이스면 `direct`를 자동 선택하고, 다른 네임스페이스나 다른 클러스터면 `stdio-bridge`를 자동 선택합니다.
- `direct`: 단일 임시 파드에 소스/타겟 PVC를 동시 마운트하여 로컬 디스크 간 초고속 복제를 수행합니다.
- `stdio-bridge`: 로컬 머신을 중계 브릿지로 사용하여 두 클러스터 간에 Stdio 파이프라인 스트리밍을 수행합니다.

---

## 주요 플래그 목록

### `kubectl clone resource` / `r`
```text
      --to-namespace string      복제 대상 네임스페이스 (기본값: 현재 컨텍스트의 기본 네임스페이스)
      --to-context string        복제 대상 kubeconfig 컨텍스트
      --to-kubeconfig string     복제 대상 kubeconfig 파일 경로
      --to-name string           복제될 리소스의 새 이름
      --overwrite                대상 리소스가 이미 존재하는 경우 덮어쓰기
      --dry-run string           Dry-run 방식 설정: 'none', 'client', 'server'
  -o, --output string            출력 형식: 'yaml' 또는 'json'
      --clear-nodeport           포트 충돌 방지를 위해 Service의 nodePort 초기화 (기본값: true)
      --preserve-pvc-volume      PVC의 spec.volumeName 유지 (기본값: false)
```

### `kubectl clone file` / `f`
```text
  -c, --container string        파드 내 대상 컨테이너 이름
      --src-context string      소스 kubeconfig 컨텍스트
  -n, --src-namespace string    소스 네임스페이스
      --dest-context string     대상 kubeconfig 컨텍스트
      --dest-namespace string   대상 네임스페이스
      --delete                  대상 경로에서 원본에 없는 파일 삭제 (미러링)
  -z, --compress                전송 중 데이터 스트림 압축 (기본값: true)
      --dry-run                 실제 변경 없이 전송될 파일 목록만 테스트
```

### `kubectl clone pvc` / `p`
```text
      --src-context string      소스 kubeconfig 컨텍스트
  -n, --src-namespace string    소스 네임스페이스
      --dest-context string     대상 kubeconfig 컨텍스트
      --dest-namespace string   대상 네임스페이스
      --src-path string         복제할 소스 PVC 내부 하위 경로 (기본값: "/")
      --dest-path string        복제될 대상 PVC 내부 하위 경로 (기본값: "/")
      --strategy string         복제 전략 선택: 'auto', 'direct', 'stdio-bridge' (기본값: "auto")
      --delete                  대상 PVC에서 원본에 없는 파일 삭제 (완전 미러링)
  -z, --compress                전송 데이터 압축 (기본값: true)
      --image string            임시 마이그레이션 파드에 사용할 컨테이너 이미지 (기본값: "alpine:latest")
      --dry-run                 실제 전송 없이 테스트 수행
```

---

## 라이선스

Apache License, Version 2.0 하에 배포됩니다. 자세한 내용은 [LICENSE](LICENSE) 파일을 참조하세요.
