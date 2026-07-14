// Package catalog defines the runnable eGovFrame repositories and how to
// clone/build/run each one. It is pure data plus PATH-detection helpers and
// must not import net/http or os/exec process management.
//
// 포트 배정 (동시 실행 시 충돌 방지):
//   - 런처 자체: 7070
//   - RSP 공유 Tomcat: 8080 (설정에서 변경 가능) — 타깃 기본 포트는 8080을 피한다
//   - 인프라 컨테이너: MySQL 3306, Redis 6379, RabbitMQ 5672
//   - 단독 실행 타깃: boot-sample 8090, web-sample 8091, simple-backend 8092,
//     portal 8093, enterprise-business 8094, homepage 8095,
//     common-components 8096, simple-react 5173
//   - msa 내부: ConfigServer 8888, Eureka 8761, EgovMain 19003,
//     EgovLogin 19004, EgovBoard 19005, Gateway 19000
//   - msa-edu 내부: config 8889, discovery 8762, apigateway 8001,
//     포털 프론트 3000 — msa와 내부 포트가 겹치지 않아 두 MSA 스택을
//     동시에 실행할 수 있다
package catalog

import "os/exec"

type Tier int

const (
	Tier1 Tier = 1
	Tier2 Tier = 2
	Tier3 Tier = 3
)

// Command is one external invocation. Name is resolved via exec.LookPath at
// run time (on Windows "mvn" resolves to mvn.cmd via PATHEXT). Dir is relative
// to the cloned target directory; "" means the target root.
type Command struct {
	Name string
	Args []string
	Dir  string
	// Port is the fixed port a Run-step dependency service binds to (e.g. a
	// config/discovery server started ahead of the target's long-running
	// process). 0 means no port wait — the runner just waits 2 seconds
	// before starting the next step.
	Port int
}

// Account is a default login account seeded by the template, surfaced in the
// UI so users don't have to dig through source/DB seed data to find one.
type Account struct {
	Label    string
	ID       string
	Password string
}

// Target is a single eGovFrame repository entry.
type Target struct {
	ID          string
	DisplayName string
	Category    string
	RepoURL     string
	Branch      string // "" = repo default branch
	Tier        Tier
	Prereqs     []string
	Port        int    // 0 = no served port
	OpenPath    string // browser path when Port>0
	Build       []Command
	Run         []Command // empty = not one-click runnable (guide only)
	Note        string
	// DeployType classifies how the target is launched:
	//   "boot" – spring-boot:run
	//   "war"  – WAR deployed to an isolated Tomcat instance
	//   "react" – npm dev server
	//   "lib"  – build-only (no one-click run)
	DeployType string
	// BackendID is the target ID of the paired backend service, if any.
	// When set, the runner injects the backend's live port into this
	// target's env at Run time, but only if the backend is currently running.
	BackendID string
	// RunEnv lists extra environment variables injected into every command
	// of the Run step (dependency services and the final long-running
	// process alike).
	RunEnv []string
	// Accounts lists the template's seeded default login accounts, if any.
	Accounts []Account
}

func mvn(args ...string) Command { return Command{Name: "mvn", Args: args} }

func Targets() []Target {
	base := "https://github.com/eGovFramework/"
	return []Target{
		{
			ID: "boot-sample", DisplayName: "부트 기반 심플 게시판 (Spring Boot)",
			Category: "심플 게시판",
			RepoURL:  base + "egovframe-boot-sample-java-config", Tier: Tier1,
			Prereqs: []string{"git", "mvn"}, Port: 8090, OpenPath: "/",
			Build:      []Command{mvn("-B", "-DskipTests", "package")},
			Run:        []Command{mvn("-B", "spring-boot:run")},
			Note:       "인프라 0. 라이브 데모 핵심.",
			DeployType: "boot",
		},
		{
			ID: "web-sample", DisplayName: "심플 게시판 (XML 기반 · WAR)",
			Category: "심플 게시판",
			RepoURL:  base + "egovframe-web-sample", Tier: Tier1,
			Prereqs: []string{"git", "mvn"}, Port: 8091, OpenPath: "/",
			Build:      []Command{mvn("-B", "-DskipTests", "package")},
			Note:       "Jakarta EE 10. WAR → Tomcat 10.1+ 배포 가이드 참고.",
			DeployType: "war",
		},
		{
			ID: "simple-backend", DisplayName: "템플릿 백엔드 (Spring Boot)",
			Category: "심플 홈페이지 세트",
			RepoURL:  base + "egovframe-template-simple-backend", Tier: Tier1,
			Prereqs: []string{"git", "mvn"}, Port: 8092, OpenPath: "/",
			Build:      []Command{mvn("-B", "-DskipTests", "package")},
			Run:        []Command{mvn("-B", "spring-boot:run")},
			Note:       "DB 필요(docker-compose.yml). DB 미기동 시 부팅 실패 가능.",
			DeployType: "boot",
			Accounts: []Account{
				{Label: "관리자", ID: "admin", Password: "Admin@1234"},
				{Label: "일반사용자", ID: "user", Password: "User@1234"},
			},
		},
		{
			ID: "simple-react", DisplayName: "템플릿 프론트엔드 (React · Vite)",
			Category: "심플 홈페이지 세트",
			RepoURL:  base + "egovframe-template-simple-react", Tier: Tier1,
			Prereqs: []string{"git", "npm"}, Port: 5173, OpenPath: "/",
			Build:      []Command{{Name: "npm", Args: []string{"ci"}}},
			Run:        []Command{{Name: "npm", Args: []string{"run", "dev"}}},
			DeployType: "react",
			BackendID:  "simple-backend",
			Accounts: []Account{
				{Label: "관리자", ID: "admin", Password: "Admin@1234"},
				{Label: "일반사용자", ID: "user", Password: "User@1234"},
			},
		},
		{
			ID: "portal", DisplayName: "포털 사이트 템플릿 (WAR)",
			Category: "일반 템플릿",
			RepoURL:  base + "egovframe-portal-site-template", Tier: Tier1,
			Prereqs: []string{"git", "mvn"}, Port: 8093, OpenPath: "/",
			Build:      []Command{mvn("-B", "-DskipTests", "package")},
			Note:       "WAR. Tomcat 10.1+ 배포.",
			DeployType: "war",
			Accounts: []Account{
				{Label: "관리자", ID: "admin", Password: "1"},
				{Label: "회원1", ID: "user1", Password: "1"},
			},
		},
		{
			ID: "enterprise-business", DisplayName: "내부업무 시스템 템플릿 (WAR)",
			Category: "일반 템플릿",
			RepoURL:  base + "egovframe-enterprise-business-template", Tier: Tier1,
			Prereqs: []string{"git", "mvn"}, Port: 8094, OpenPath: "/",
			Build:      []Command{mvn("-B", "-DskipTests", "package")},
			Note:       "WAR. Tomcat 10.1+ 배포.",
			DeployType: "war",
			Accounts: []Account{
				{Label: "관리자", ID: "admin", Password: "1"},
				{Label: "유저1", ID: "user1", Password: "1"},
			},
		},
		{
			ID: "homepage", DisplayName: "심플 홈페이지 템플릿 (WAR)",
			Category: "일반 템플릿",
			RepoURL:  base + "egovframe-simple-homepage-template", Tier: Tier1,
			Prereqs: []string{"git", "mvn"}, Port: 8095, OpenPath: "/",
			Build:      []Command{mvn("-B", "-DskipTests", "package")},
			Note:       "WAR. Tomcat 10.1+ 배포.",
			DeployType: "war",
			Accounts: []Account{
				{Label: "관리자", ID: "admin", Password: "1"},
			},
		},
		{
			ID: "msa", DisplayName: "MSA 공통컴포넌트",
			Category: "공통컴포넌트 & MSA",
			RepoURL:  base + "egovframe-msa-common-components", Tier: Tier3,
			Prereqs: []string{"git", "mvn", "docker"}, Port: 19000, OpenPath: "/",
			Build: []Command{mvn("-B", "-DskipTests", "package")},
			Run: []Command{
				{Name: "java", Args: []string{"-jar", "ConfigServer/target/ConfigServer.jar"}, Port: 8888},
				{Name: "java", Args: []string{"-jar", "EurekaServer/target/EurekaServer.jar"}, Port: 8761},
				{Name: "java", Args: []string{"-jar", "EgovMain/target/EgovMain.jar"}, Port: 19003},
				{Name: "java", Args: []string{"-jar", "EgovLogin/target/EgovLogin.jar"}, Port: 19004},
				{Name: "java", Args: []string{"-jar", "EgovBoard/target/EgovBoard.jar"}, Port: 19005},
				{Name: "java", Args: []string{"-jar", "GatewayServer/target/GatewayServer.jar"}},
			},
			Note:       "Run 실행 시 Config/Eureka/Main/Login/Board/Gateway 모듈을 순차적으로 자동 기동합니다.",
			DeployType: "boot",
			// 로컬 데모에 Zipkin(9411)이 없어 트레이싱 export를 끔.
			RunEnv: []string{"MANAGEMENT_ZIPKIN_TRACING_EXPORT_ENABLED=false"},
			Accounts: []Account{
				{Label: "업무사용자(업무 탭)", ID: "TEST1", Password: "rhdxhd12"},
				{Label: "웹마스터(업무 탭)", ID: "webmaster", Password: "rhdxhd12"},
				{Label: "일반회원(일반 탭)", ID: "USER", Password: "rhdxhd12"},
				{Label: "기업회원(기업 탭)", ID: "ENTERPRISE", Password: "rhdxhd12"},
			},
		},
		{
			ID: "common-components", DisplayName: "공통컴포넌트 (WAR)",
			Category: "공통컴포넌트 & MSA",
			RepoURL:  base + "egovframe-common-components", Tier: Tier1,
			Prereqs: []string{"git", "mvn"}, Port: 8096, OpenPath: "/",
			Build:      []Command{mvn("-B", "-DskipTests", "package")},
			Note:       "WAR. Tomcat 10.1+ 배포.",
			DeployType: "war",
			Accounts: []Account{
				{Label: "업무사용자", ID: "TEST1", Password: "rhdxhd12"},
				{Label: "웹마스터", ID: "webmaster", Password: "rhdxhd12"},
				{Label: "일반회원", ID: "USER", Password: "rhdxhd12"},
				{Label: "기업회원", ID: "ENTERPRISE", Password: "rhdxhd12"},
			},
		},
		{
			ID: "msa-edu", DisplayName: "MSA 템플릿 (교육용)",
			Category: "공통컴포넌트 & MSA",
			RepoURL:  base + "egovframe-msa-edu", Tier: Tier3,
			Prereqs: []string{"git", "docker", "java", "npm"}, Port: 3000, OpenPath: "/",
			Build: []Command{
				// module-common은 gradle-wrapper.jar가 레포에 없어 이웃 서비스의
				// wrapper를 빌려 실행한다 (gradle 프로젝트 디렉터리는 cwd 기준).
				{Name: "sh", Args: []string{"../config/gradlew", "publishToMavenLocal", "-x", "test"}, Dir: "backend/egovframe-cloud-module-common"},
				{Name: "sh", Args: []string{"gradlew", "build", "-x", "test"}, Dir: "backend/config"},
				{Name: "sh", Args: []string{"gradlew", "build", "-x", "test"}, Dir: "backend/discovery"},
				{Name: "sh", Args: []string{"gradlew", "build", "-x", "test"}, Dir: "backend/apigateway"},
				{Name: "sh", Args: []string{"gradlew", "build", "-x", "test"}, Dir: "backend/user-service"},
				{Name: "sh", Args: []string{"gradlew", "build", "-x", "test"}, Dir: "backend/portal-service"},
				{Name: "sh", Args: []string{"gradlew", "build", "-x", "test"}, Dir: "backend/board-service"},
				// Next 11 프론트 — 구형 peer 의존성 때문에 legacy-peer-deps 필요.
				{Name: "npm", Args: []string{"install", "--legacy-peer-deps"}, Dir: "frontend/portal"},
			},
			Run: []Command{
				{Name: "java", Args: []string{"-jar", "backend/config/build/libs/config-5.0.0.jar"}, Port: 8889},
				{Name: "java", Args: []string{"-jar", "backend/discovery/build/libs/discovery-5.0.0.jar"}, Port: 8762},
				{Name: "java", Args: []string{"-jar", "backend/user-service/build/libs/user-service-5.0.0.jar"}},
				{Name: "java", Args: []string{"-jar", "backend/portal-service/build/libs/portal-service-5.0.0.jar"}},
				{Name: "java", Args: []string{"-jar", "backend/board-service/build/libs/board-service-5.0.0.jar"}},
				{Name: "java", Args: []string{"-jar", "backend/apigateway/build/libs/apigateway-5.0.0.jar"}, Port: 8001},
				// 포털 프론트(Next.js custom server, PORT env 사용)가 대표 프로세스.
				{Name: "npm", Args: []string{"run", "dev"}, Dir: "frontend/portal"},
			},
			Note:       "원클릭 Run: 백엔드 6서비스(설정→디스커버리→서비스 3종→게이트웨이 8001) 순차 기동 후 포털 프론트(3000)를 띄웁니다. 첫 Run은 Gradle/npm 다운로드로 오래 걸립니다. 프론트 로그인: 1@gmail.com / test1234! msa-edu 내부 포트는 8889/8762/8001로 이동되어 msa와 두 MSA 스택을 동시에 띄울 수 있습니다.",
			DeployType: "boot",
			RunEnv: []string{
				// 로컬 데모에 Zipkin(9411)이 없어 트레이싱 export를 끔.
				"MANAGEMENT_ZIPKIN_TRACING_EXPORT_ENABLED=false",
				// 구형 Next·webpack이 OpenSSL 3에서 ERR_OSSL_EVP_UNSUPPORTED로 죽는 것 방지.
				"NODE_OPTIONS=--openssl-legacy-provider",
				// msa와 동시 실행하기 위해 내부 포트를 8889/8762/8001로 이동 (config/discovery/apigateway).
				"SPRING_CLOUD_CONFIG_URI=http://localhost:8889",
				"EUREKA_CLIENT_SERVICEURL_DEFAULTZONE=http://admin:admin@localhost:8762/eureka",
				"SERVER_API_URL=http://localhost:8001",
			},
			Accounts: []Account{
				{Label: "관리자(프론트)", ID: "1@gmail.com", Password: "test1234!"},
			},
		},
	}
}

func ByID(id string) (Target, bool) {
	for _, t := range Targets() {
		if t.ID == id {
			return t, true
		}
	}
	return Target{}, false
}

// Available reports whether tool is resolvable on PATH.
func Available(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}
