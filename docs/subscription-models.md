# 구독 모델 연결 / Subscription model connections

LLM 설정에서 `ChatGPT 구독` 또는 `Claude 구독 (Claude Code)`를 선택할 수 있습니다.
기존 API 키 설정과 함께 저장하고 에이전트·작업에 지정할 수 있습니다. 에이전트 역할,
도구 실행·권한 검사, 대화 기록, 결과 저장은 기존 ARTEX 경로를 사용합니다.

## 등록 절차 비교

| 방식 | 등록 순서 | 인증 정보 관리 |
| --- | --- | --- |
| API 키 | 키 발급 → 공급자/API 주소/키 입력 → 모델 선택 → 테스트 → 저장·활성화 | 기존 API 키 저장 방식 |
| ChatGPT 구독 | **Continue with ChatGPT** → **OpenAI 로그인 열기** → 로그인·구독 사용 승인 → 모델 목록 불러오기 → 테스트 → 저장·활성화 | 서버의 비공개 파일; 자동 갱신 |
| Claude 구독 | 실행 환경에 공식 Claude Code 설치 → 최초 공식 로그인 → 연결 상태 새로고침 → 모델 선택 → 테스트 → 저장·활성화 | Claude Code가 인증·갱신 관리 |

각 서비스의 모델 접근 권한과 구독 한도가 적용됩니다. 로그인 성공만으로 모델 호출 성공을
보장하지 않으므로 **연결 테스트**에서 실제 응답을 확인하세요. 구독 한도 소진 시 유료 API로
전환하려면 기존 ARTEX 설정 체인에 API 프로파일을 명시적으로 넣어야 합니다.

## ChatGPT

ARTEX가 실행되는 PC의 브라우저에서 구독 연결 패널을 열고 OpenAI 로그인과
**Use your ChatGPT plan** 권한 승인을 완료하세요. 새 모델 설정에서 `ChatGPT 구독`을
선택하고 새로고침 버튼으로 계정에서 사용 가능한 모델을 불러온 뒤 테스트·저장·활성화합니다.

여러 계정을 등록할 수 있으며 **모든 ChatGPT 프로파일은 패널의 활성 계정**을 사용합니다.
같은 이메일이라도 등록 ID가 다른 연결은 구분됩니다. 계정을 전환하면 모델 목록을 다시
불러오세요. 연결 해제는 로컬 토큰 삭제와 원격 갱신 세션 해제를 시도합니다. 원격 해제 확인에
실패하면 ChatGPT 설정에서 직접 해제하라는 안내가 표시됩니다.

인증 콜백은 `http://127.0.0.1:1455/auth/callback`입니다. Docker Compose는 호스트의
`127.0.0.1`에만 이 포트를 게시합니다. state·PKCE·nonce와 ID 토큰 서명·issuer·audience·만료를
검증합니다. 다른 PC에서 접속하는 원격 웹 UI에는 이 로컬 로그인 절차를 사용하지 마세요.

토큰은 `<data>/subscriptions/chatgpt.json`에 원자적으로 저장하며 Unix 권한은 `0600`입니다.
브라우저에는 표시용 계정 정보와 연결 상태만 전달됩니다. 이 디렉터리는 비공개로 백업하고
같은 파일을 여러 ARTEX 프로세스에서 동시에 사용하지 마세요.

공식 Responses API의 `store:false`, `stream:true`를 사용합니다. 전체 대화와 도구 결과를
매 요청에 포함하고 함수 도구는 `artex` 네임스페이스에 넣습니다. `response.completed` 없는
응답은 실패로 처리합니다. 이 경로에서 지원하지 않는 출력 토큰 상한과 temperature 등은
전송하지 않습니다. Codex·ChatGPT Work와 구독 사용 한도를 공유합니다.

공식 문서: [로그인](https://developers.openai.com/siwc/token-sharing-open-source/sign-in),
[세션](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions),
[모델 호출](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference),
[제한](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations).

## Claude

업데이트된 Docker 이미지에는 공식 Claude Code가 포함됩니다. 프로젝트 폴더에서 실행합니다.

```sh
docker compose exec artex claude auth login
docker compose exec artex claude auth status
```

본인의 Claude 구독 계정을 선택하세요. PC와 컨테이너의 로그인은 별개입니다.
연결 해제는 `docker compose exec artex claude auth logout`으로 합니다.
인증 데이터는 영속 볼륨의 `/app/data/subscriptions/claude`에 저장됩니다.

Docker 없이 실행하면 공식 CLI를 설치하고 ARTEX와 같은 설정 디렉터리에서 로그인합니다.

```sh
export CLAUDE_CONFIG_DIR="/absolute/path/to/artex/data/subscriptions/claude"
claude auth login
```

ARTEX도 같은 `CLAUDE_CONFIG_DIR`로 실행하세요. 실행 파일을 별도로 지정할 때는 서버 환경 변수
`ARTEX_CLAUDE_BIN`을 사용합니다. CLI 원본과 자체 로그인 선택지는 변경하지 않습니다.
구독 프로파일 호출에는 전역 API 키·커스텀 API 주소를 전달하지 않아 예상 밖의 API 과금을
방지합니다. API 키 사용은 기존 Anthropic 형식을 선택하세요.

Claude Code의 기본 도구·MCP 실행은 꺼 두고 구조화된 응답으로 도구 호출 의도를 받아
ARTEX의 기존 실행기로 전달합니다. 알 수 없는 도구나 잘못된 인자는 오류로 처리합니다.
프로세스 실행과 구조화 출력 변환이 추가되므로 API 방식과 속도·토큰 사용량·모델 동작이
완전히 같지는 않습니다. 결과는 모델 턴이 끝난 후 전달되며 요청별 최대 실행 시간은 5분입니다.
작업 취소는 자식 프로세스에도 전달됩니다. 설정별 요청 속도와 출력 토큰 상한은 지원하지 않습니다.
Claude의 사고 모드·강도는 CLI 기본값을 사용하며 ARTEX 설정에서 별도로 지정하지 않습니다.

공식 문서: [CLI](https://code.claude.com/docs/en/cli-reference),
[프로그램 호출](https://code.claude.com/docs/en/headless),
[제품 내 실행 조건](https://code.claude.com/docs/en/legal-and-compliance).

## Deployment and validation

Build the static UI and embedded Go binary using the repository build instructions, then build
the Docker image. Recreate only `artex` so port 1455 and the new environment take effect.
Preserve `data/` and PostgreSQL volumes. For downgrades, remove/convert subscription profiles
first or restore a matching database backup; older schemas do not accept the new formats.

Local tests cover authorization state, ID-token rejection, serialized refresh and persistence,
credential redaction, incomplete SSE failures, tool-call/result continuity, and database migration.
They consume no subscription usage. Actual account eligibility and inference require the owner's
interactive sign-in and connection test. Never commit credentials or local test results.
