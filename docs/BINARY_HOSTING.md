# Binary Hosting (R2 / S3)

GitHub 저장소가 비공개여도 에이전트 바이너리를 공개 URL로 배포하려면 R2 또는 S3에 올리면 됩니다.

---

## 방법 1: Cloudflare R2 (추천)

R2는 이그레스 비용 없음, S3 호환 API 사용.

### 1) R2 버킷 만들기

1. [Cloudflare Dashboard](https://dash.cloudflare.com) → **R2** → **Create bucket**
2. 이름 예: `socquery-agent-binaries`
3. **Create bucket**

### 2) 퍼블릭 접근 설정

1. 해당 버킷 → **Settings** → **Public access**
2. **Allow Access** 켜기
3. **R2.dev subdomain** 사용 또는 **Custom domain** 연결
   - R2.dev: `https://pub-xxxx.r2.dev` 형태의 URL 제공
   - Custom: 예) `https://dl.yourdomain.com` → 버킷에 연결

이후 파일 URL: `{공개 베이스 URL}/{객체 키}`  
예: `https://pub-xxxx.r2.dev/socquery-agent-binaries/releases/latest/soc-darwin-arm64`

### 3) R2 API 토큰

1. R2 → **Manage R2 API Tokens** → **Create API token**
2. 권한: **Object Read & Write**
3. **Create API Token** 후 **Access Key ID**, **Secret Access Key** 복사

### 4) GitHub Secrets

저장소 **Settings** → **Secrets and variables** → **Actions**에서 추가:

| Name | Value |
|------|--------|
| `R2_ACCESS_KEY_ID` | 위 Access Key ID |
| `R2_SECRET_ACCESS_KEY` | 위 Secret Access Key |
| `R2_BUCKET` | 버킷 이름 (예: socquery-agent-binaries) |
| `R2_ACCOUNT_ID` | Cloudflare 계정 ID (대시보드 오른쪽 아래 또는 Workers/R2 페이지 URL에 있음) |
| `R2_PUBLIC_URL_BASE` | 퍼블릭 베이스 URL (예: `https://pub-xxxx.r2.dev/socquery-agent-binaries`) |

### 5) 프론트엔드 환경 변수

soquery-saas-frontend 배포 시:

- **NEXT_PUBLIC_AGENT_RELEASE_URL** = `{R2_PUBLIC_URL_BASE}/releases/latest`  
  예: `https://pub-xxxx.r2.dev/socquery-agent-binaries/releases/latest`

이렇게 하면 대시보드의 다운로드 명령이 `curl -o soc-darwin-arm64 "${NEXT_PUBLIC_AGENT_RELEASE_URL}/soc-darwin-arm64"` 형태로 이 URL을 쓰게 됩니다.

---

## 방법 2: AWS S3

이미 AWS를 쓰는 경우 S3로 같은 방식 적용 가능.

### 1) S3 버킷

1. S3 → **Create bucket**
2. 버킷 정책에서 **퍼블릭 읽기** 허용 (또는 CloudFront로만 배포)

예시 정책 (버킷 이름·경로는 본인에 맞게 수정):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "PublicRead",
      "Effect": "Allow",
      "Principal": "*",
      "Action": "s3:GetObject",
      "Resource": "arn:aws:s3:::YOUR-BUCKET/releases/*"
    }
  ]
}
```

### 2) IAM 사용자

1. IAM → 사용자 생성 → **Programmatic access**
2. 권한: 해당 버킷에 `s3:PutObject`, `s3:GetObject` 등
3. **Access Key ID**, **Secret Access Key** 저장

### 3) GitHub Secrets

| Name | Value |
|------|--------|
| `AWS_ACCESS_KEY_ID` | IAM Access Key ID |
| `AWS_SECRET_ACCESS_KEY` | IAM Secret Access Key |
| `AWS_S3_BUCKET` | 버킷 이름 |
| `AWS_REGION` | 예: ap-northeast-2 |

퍼블릭 URL:  
`https://YOUR-BUCKET.s3.REGION.amazonaws.com/releases/latest/soc-darwin-arm64`  
또는 CloudFront 도메인을 쓰면 그걸 `NEXT_PUBLIC_AGENT_RELEASE_URL` 로 설정.

### 4) 워크플로우

R2 대신 S3용 job을 쓰려면 `.github/workflows/release.yml`에서 R2 단계를 S3 업로드로 바꾸면 됩니다. (예: `aws s3 cp dist/ s3://BUCKET/releases/latest/ --recursive`)

---

## 릴리스 후 확인

- R2/S3에 `releases/latest/soc-darwin-arm64` 등이 올라갔는지 확인
- 브라우저 또는 `curl -I "공개_URL/releases/latest/soc-darwin-arm64"` 로 200 응답 확인
- 대시보드에서 등록 토큰 발급 후 표시되는 curl 명령으로 실제 다운로드 테스트
