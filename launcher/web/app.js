const cardsEl = document.getElementById("cards");
const logEl = document.getElementById("log");
const logTitle = document.getElementById("logTitle");
const globalEl = document.getElementById("globalPrereqs");

// --- 로그 콘솔: 서비스별 필터 · 줄바꿈 토글 · 전체화면 ---
// MSA처럼 여러 서브 서비스의 출력이 한 버퍼에 섞이는 경우, Spring Boot 로그의
// "--- [서비스명] [스레드]" 패턴에서 서비스명을 자동 수집해 드롭다운으로 필터링한다.
let logLines = [];
let logFilter = "";
const svcSelect = document.getElementById("logServiceFilter");
const svcSeen = new Set();

function noteService(line) {
  const m = line.match(/--- \[([\w.-]+)\] \[/);
  if (m && !svcSeen.has(m[1])) {
    svcSeen.add(m[1]);
    const opt = document.createElement("option");
    opt.value = m[1];
    opt.textContent = m[1];
    svcSelect.appendChild(opt);
  }
}

function lineMatches(line) {
  return !logFilter || line.includes(`[${logFilter}]`);
}

function rebuildLog() {
  logEl.textContent = logLines.filter(lineMatches).map((l) => l + "\n").join("");
  logEl.scrollTop = logEl.scrollHeight;
}

function resetLogConsole() {
  logLines = [];
  logFilter = "";
  svcSeen.clear();
  svcSelect.innerHTML = '<option value="">전체 서비스</option>';
  logEl.textContent = "";
}

svcSelect.onchange = () => {
  logFilter = svcSelect.value;
  rebuildLog();
};

const wrapBtn = document.getElementById("toggleWrapBtn");
let wrapOn = localStorage.getItem("logWrap") !== "off";
function applyWrap() {
  logEl.classList.toggle("nowrap", !wrapOn);
  wrapBtn.textContent = wrapOn ? "↩ 줄바꿈 켜짐" : "→ 줄바꿈 꺼짐";
}
wrapBtn.onclick = () => {
  wrapOn = !wrapOn;
  localStorage.setItem("logWrap", wrapOn ? "on" : "off");
  applyWrap();
};
applyWrap();

const consoleEl = document.querySelector(".console");
const fsBtn = document.getElementById("fullscreenLog");
fsBtn.onclick = () => {
  const on = consoleEl.classList.toggle("fullscreen");
  fsBtn.textContent = on ? "✕ 전체화면 닫기" : "⛶ 전체화면";
  logEl.scrollTop = logEl.scrollHeight;
};
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && consoleEl.classList.contains("fullscreen")) fsBtn.onclick();
  if (e.key === "Escape" && !document.getElementById("accountsModal").classList.contains("hidden")) closeAccountsModal();
  if (e.key === "Escape" && !document.getElementById("tomcatModal").classList.contains("hidden")) closeTomcatModal();
});

document.getElementById("clearLog").onclick = () => { logLines = []; logEl.textContent = ""; };
document.getElementById("copyLog").onclick = async () => {
  const btn = document.getElementById("copyLog");
  const origText = btn.textContent;
  try {
    await navigator.clipboard.writeText(logEl.textContent);
    btn.textContent = "copied!";
    setTimeout(() => { btn.textContent = origText; }, 1500);
  } catch (err) {
    console.error("Failed to copy: ", err);
    const textarea = document.createElement("textarea");
    textarea.value = logEl.textContent;
    textarea.style.position = "fixed"; // Avoid scrolling to bottom
    document.body.appendChild(textarea);
    textarea.select();
    try {
      document.execCommand("copy");
      btn.textContent = "copied!";
      setTimeout(() => { btn.textContent = origText; }, 1500);
    } catch (e) {
      alert("로그 복사에 실패했습니다.");
    }
    document.body.removeChild(textarea);
  }
};

let evtSource = null;
let currentViews = [];
// 사용자가 입력한 포트를 타깃별로 기억 (폴링 리렌더 시 입력값 유지)
const customPorts = {};
// 카드 제목 끝의 괄호 부속어("(Spring Boot)" 등) — 중간에서 쪼개지지 않고
// 괄호 앞에서만 개행되도록 감싼다. 1.5초 폴링마다 카드 수만큼 쓰이므로 모듈 상수.
const TITLE_PAREN_RE = /\s*(\([^)]*\))\s*$/;

async function api(path, method = "GET", body = null) {
  const options = { method };
  if (body) {
    options.headers = { "Content-Type": "application/json" };
    options.body = JSON.stringify(body);
  }
  return fetch(path, options);
}

let jdkList = [];

async function loadJDKs(currentHome) {
  const res = await api("/api/jdks");
  if (!res.ok) return;
  jdkList = await res.json();
  const sel = document.getElementById("jdkSelect");
  sel.innerHTML = "";
  if (jdkList.length === 0) {
    const opt = document.createElement("option");
    opt.value = "";
    opt.textContent = "JDK를 찾을 수 없습니다";
    sel.appendChild(opt);
    return;
  }

  // Group by major version
  const groups = {};
  jdkList.forEach(jdk => {
    if (!groups[jdk.version]) groups[jdk.version] = [];
    groups[jdk.version].push(jdk);
  });
  // Render descending (latest first for visibility) but set value AFTER DOM is built
  const sortedVersions = Object.keys(groups).map(Number).sort((a, b) => b - a);
  sortedVersions.forEach(ver => {
    const grp = document.createElement("optgroup");
    grp.label = `Java ${ver}`;
    groups[ver].forEach(jdk => {
      const opt = document.createElement("option");
      opt.value = jdk.home;
      opt.textContent = jdk.label;
      grp.appendChild(opt);
    });
    sel.appendChild(grp);
  });

  // Set value AFTER all options are in DOM to avoid browser auto-selection interference
  if (currentHome) {
    sel.value = currentHome;
  }
  // Fallback: prefer JDK 17, else highest available
  if (!sel.value || sel.value !== currentHome) {
    const jdk17 = jdkList.find(j => j.version === 17);
    sel.value = jdk17 ? jdk17.home : jdkList[jdkList.length - 1].home;
  }
  updateJdkHomePath();
}

function updateJdkHomePath() {
  const sel = document.getElementById("jdkSelect");
  const pathEl = document.getElementById("jdkHomePath");
  pathEl.textContent = sel.value ? "JAVA_HOME: " + sel.value : "";
}
document.getElementById("jdkSelect").onchange = updateJdkHomePath;

async function loadConfig() {
  const res = await api("/api/config");
  if (res.ok) {
    const config = await res.json();
    document.getElementById("vscodePathInput").value = config.vscodePath;
    document.getElementById("workspacePathInput").value = config.workspacePath;
    document.getElementById("tomcatPathInput").value = config.tomcatPath || "";
    document.getElementById("rspTomcatPortInput").value = config.rspTomcatPort || 8080;
    document.getElementById("skipTestsInput").checked = config.skipTests;
    await loadJDKs(config.javaHome);
  }
}

function getCommonConfig() {
  return {
    workspacePath: document.getElementById("workspacePathInput").value,
    tomcatPath: document.getElementById("tomcatPathInput").value,
    rspTomcatPort: parseInt(document.getElementById("rspTomcatPortInput").value, 10) || 8080,
    vscodePath: document.getElementById("vscodePathInput").value,
    skipTests: document.getElementById("skipTestsInput").checked,
    javaHome: document.getElementById("jdkSelect").value
  };
}

document.getElementById("saveJdk").onclick = async () => {
  const res = await api("/api/config", "POST", getCommonConfig());
  if (res.ok) {
    updateJdkHomePath();
    alert("JDK가 설정되었습니다: " + document.getElementById("jdkSelect").value);
  } else {
    alert(await res.text());
  }
};

document.getElementById("skipTestsInput").onchange = async () => {
  const res = await api("/api/config", "POST", getCommonConfig());
  if (!res.ok) {
    alert(await res.text());
  }
};

document.getElementById("saveVSCode").onclick = async () => {
  const res = await api("/api/config", "POST", getCommonConfig());
  if (res.ok) {
    alert("VSCode 실행 경로가 설정되었습니다.");
    refresh();
  } else {
    alert(await res.text());
  }
};

document.getElementById("saveConfig").onclick = async () => {
  const res = await api("/api/config", "POST", getCommonConfig());
  if (res.ok) {
    alert("저장소 경로가 설정되었습니다.");
    refresh();
  } else {
    alert(await res.text());
  }
};

document.getElementById("saveTomcat").onclick = async () => {
  const res = await api("/api/config", "POST", getCommonConfig());
  if (res.ok) {
    alert("Tomcat 설치 경로가 설정되었습니다.");
    refresh();
  } else {
    alert(await res.text());
  }
};

document.getElementById("saveRspPort").onclick = async () => {
  const res = await api("/api/config", "POST", getCommonConfig());
  if (res.ok) {
    alert("RSP 공유 Tomcat 포트가 설정되었습니다. 다음 'RSP 자동기동 (VSCode)' 실행 시 반영됩니다.");
    refresh();
  } else {
    alert(await res.text());
  }
};

function actionButton(label, cls, enabled, onClick) {
  const b = document.createElement("button");
  b.textContent = label;
  if (cls) b.className = cls;
  b.disabled = !enabled;
  b.onclick = onClick;
  return b;
}

let collapsedCategories = JSON.parse(localStorage.getItem("egov_collapsed_cats") || "{}");

function toggleCategoryCollapse(catName) {
  collapsedCategories[catName] = !collapsedCategories[catName];
  localStorage.setItem("egov_collapsed_cats", JSON.stringify(collapsedCategories));
  if (currentViews) {
    render(currentViews);
  }
}

function streamLogs(id, title) {
  if (evtSource) evtSource.close();
  resetLogConsole();
  logTitle.textContent = `로그 — ${title}`;
  evtSource = new EventSource(`/api/events/${id}`);
  evtSource.onmessage = (e) => {
    logLines.push(e.data);
    noteService(e.data);
    if (lineMatches(e.data)) {
      logEl.textContent += e.data + "\n";
      logEl.scrollTop = logEl.scrollHeight;
    }
  };
}

function render(views) {
  currentViews = views;
  const tools = {};
  views.forEach((v) => Object.assign(tools, v.prereqs));
  globalEl.innerHTML = Object.entries(tools)
    .map(([k, ok]) => `<span>${k} ${ok ? "✓" : "✗"}</span>`).join("");

  // Group by Category
  const categories = {};
  for (const v of views) {
    const cat = v.Category || "기타";
    if (!categories[cat]) {
      categories[cat] = [];
    }
    categories[cat].push(v);
  }

  cardsEl.innerHTML = "";
  for (const [catName, list] of Object.entries(categories)) {
    const isCollapsed = !!collapsedCategories[catName];

    const catHeader = document.createElement("h2");
    catHeader.className = "category-title";

    const titleText = document.createElement("span");
    titleText.textContent = `${catName} (${list.length})`;
    catHeader.appendChild(titleText);

    const toggleBtn = document.createElement("button");
    toggleBtn.className = "btn-icon cat-toggle-btn";
    toggleBtn.type = "button";
    toggleBtn.textContent = isCollapsed ? "▼" : "▲";
    toggleBtn.title = "접기/펼치기";
    toggleBtn.setAttribute("aria-label", `${catName} 접기/펼치기`);
    toggleBtn.setAttribute("aria-expanded", String(!isCollapsed));
    catHeader.appendChild(toggleBtn);

    catHeader.onclick = (e) => {
      e.stopPropagation();
      toggleCategoryCollapse(catName);
    };

    cardsEl.appendChild(catHeader);

    const catGrid = document.createElement("div");
    catGrid.className = "category-grid" + (isCollapsed ? " collapsed" : "");

    for (const v of list) {
      const missing = Object.values(v.prereqs).some((ok) => !ok);
      const card = document.createElement("div");
      card.className = "card";

      const head = document.createElement("div");
      head.className = "card-head";
      const titleHTML = v.DisplayName.replace(TITLE_PAREN_RE, ' <span class="title-paren">$1</span>');
      head.innerHTML =
        `<span class="card-title">${titleHTML}</span>` +
        `<span class="tier">Tier ${v.Tier}</span>` +
        `<span class="badge ${v.state.status}">${v.state.status}` +
        (v.state.port && v.state.status === "running" ? ` :${v.state.port}` : "") +
        `</span>`;
      card.appendChild(head);

      if (v.Note) {
        const note = document.createElement("div");
        note.className = "note";
        note.textContent = v.Note;
        card.appendChild(note);
      }

      const act = document.createElement("div");
      act.className = "actions";
      const busy = ["cloning", "building", "running"].includes(v.state.status);
      const canOpenVSCode = v.state.status !== "idle" && v.state.status !== "cloning";
      // Clone은 항상 제공. 이미 클론돼 있으면 클릭 시 재클론 여부를 확인.
      act.appendChild(actionButton("Clone", "", !missing && !busy, () => trigger(v.ID, "clone", v.DisplayName)));
      act.appendChild(actionButton("VSCode", "", canOpenVSCode, () => trigger(v.ID, "vscode", v.DisplayName)));
      act.appendChild(actionButton("Build", "", v.cloned && !missing && !busy && v.Build?.length, () => trigger(v.ID, "build", v.DisplayName)));

      let portInput = null;
      const needsPort = (v.DeployType === "war") || (v.Run && v.Run.length > 0);
      if (needsPort) {
        const wrapper = document.createElement("div");
        wrapper.className = "port-input-wrapper";
        const lbl = document.createElement("label");
        lbl.textContent = (v.DeployType === "war") ? "Port (런처 전용):" : "Port:";
        portInput = document.createElement("input");
        portInput.type = "number";
        portInput.min = "1024";
        portInput.max = "65535";
        if (v.state.status === "running" && v.state.port) {
          portInput.value = v.state.port;
        } else {
          portInput.value = customPorts[v.ID] ?? v.Port;
        }
        portInput.oninput = () => { customPorts[v.ID] = portInput.value; };
        lbl.appendChild(portInput);
        wrapper.appendChild(lbl);
        act.appendChild(wrapper);
      }

      if (v.DeployType === "war") {
        // 경로 1: 런처가 직접 격리 Tomcat 인스턴스에 배포·기동
        act.appendChild(actionButton("▶ Tomcat 기동 (런처)", "primary", v.cloned && !missing && !busy, () => {
          const portVal = portInput ? parseInt(portInput.value, 10) : null;
          trigger(v.ID, "tomcat", v.DisplayName, portVal);
        }));
        // RSP(Community Server Connectors)에 Tomcat 서버를 자동 생성·기동·배포 후 VSCode 오픈.
        // 확장 미설치 시에는 VSCode 오픈 + 설치 안내 모달로 폴백한다.
        act.appendChild(actionButton("RSP 자동기동 (VSCode)", "", v.cloned && !missing && !busy, async () => {
          let installed = false;
          try {
            const r = await api("/api/extension-status");
            installed = r.ok && (await r.json()).installed;
          } catch (_) { /* ignore */ }
          if (!installed) {
            await trigger(v.ID, "vscode", v.DisplayName);
            openTomcatModal();
            return;
          }
          trigger(v.ID, "rsp-setup", v.DisplayName);
        }));
        const rspNote = document.createElement("div");
        rspNote.className = "note";
        rspNote.textContent = "런처: 프로젝트별 격리 Tomcat(포트 지정 가능) · RSP 자동기동: VSCode 연동 공유 Tomcat(포트는 설정 패널에서 변경, context path로 구분)";
        act.appendChild(rspNote);
      } else {
        act.appendChild(actionButton("▶ Run", "primary", v.cloned && !missing && !busy && (v.Run?.length > 0 || v.DeployType === "boot" || v.DeployType === "jar"), () => {
          const portVal = portInput ? parseInt(portInput.value, 10) : null;
          trigger(v.ID, "run", v.DisplayName, portVal);
        }));
      }
      if (v.needsDB) {
        act.appendChild(actionButton("DB 설정 (Docker MySQL)", "", v.cloned && !missing && !busy, () => trigger(v.ID, "db-setup", v.DisplayName)));
      }
      if (v.Accounts?.length > 0) {
        act.appendChild(actionButton("계정", "", true, () => openAccountsModal(v)));
      }
      act.appendChild(actionButton("■ Stop", "", v.state.status === "running", () => trigger(v.ID, "stop", v.DisplayName)));
      if (v.state.port && v.state.status === "running") {
        act.appendChild(actionButton("Open", "", true, () => window.open(`http://localhost:${v.state.port}${v.state.openPath || v.OpenPath || "/"}`)));
      }
      act.appendChild(actionButton("로그", "", true, () => streamLogs(v.ID, v.DisplayName)));
      card.appendChild(act);
      catGrid.appendChild(card);
    }
    cardsEl.appendChild(catGrid);
  }
}

async function trigger(id, action, title, port = null) {
  let url = `/api/targets/${id}/${action}`;
  const params = [];
  if (action === "clone") {
    const target = currentViews.find(v => v.ID === id);
    if (target && target.cloned) {
      const ok = confirm("이미 소스 코드가 존재합니다.\n\n기존 소스를 '완전 삭제'하고 새로 클론하시겠습니까?\n[확인] - 완전 삭제 후 새로 클론\n[취소] - 기존 소스 그대로 유지");
      params.push(`clean=${ok}`);
    }
  } else if ((action === "run" || action === "tomcat") && port) {
    params.push(`port=${port}`);
  }
  if (params.length > 0) {
    url += "?" + params.join("&");
  }
  const res = await api(url, "POST");
  if (!res.ok) { alert(await res.text()); return; }
  if (action !== "stop" && action !== "vscode") streamLogs(id, title);
}

function showModal(id) {
  document.getElementById(id).classList.remove("hidden");
  document.getElementById(id + "Backdrop").classList.remove("hidden");
}

function hideModal(id) {
  document.getElementById(id).classList.add("hidden");
  document.getElementById(id + "Backdrop").classList.add("hidden");
}

async function openTomcatModal() {
  showModal("tomcatModal");
  await updateModalExtState();
}

// Reflect install status inside the modal: hide the install button/step when
// the extension is already installed so it doesn't look like a required step.
async function updateModalExtState() {
  const line = document.getElementById("tomcatModalExtStatus");
  const installBtn = document.getElementById("tomcatModalInstall");
  const installStep = document.getElementById("tomcatStepInstall");
  let installed = false;
  try {
    const res = await api("/api/extension-status");
    if (res.ok) installed = (await res.json()).installed;
  } catch (_) {
    /* leave installed=false */
  }
  if (installed) {
    line.textContent = "✓ Community Server Connectors 설치됨 — 설치 단계는 건너뛰고 아래 기동 단계를 따르세요.";
    line.style.color = "var(--success)";
    installBtn.classList.add("hidden");
    if (installStep) installStep.classList.add("hidden");
  } else {
    line.textContent = "미설치 — 먼저 아래 설치 버튼을 누르세요.";
    line.style.color = "var(--muted)";
    installBtn.classList.remove("hidden");
    if (installStep) installStep.classList.remove("hidden");
  }
}

function closeTomcatModal() {
  hideModal("tomcatModal");
}

document.getElementById("tomcatModalClose").onclick = closeTomcatModal;
document.getElementById("tomcatModalBackdrop").onclick = closeTomcatModal;

function openAccountsModal(v) {
  document.getElementById("accountsModalTitle").textContent = `${v.DisplayName} 기본 계정`;
  const body = document.getElementById("accountsModalBody");
  body.innerHTML = "";
  for (const acc of v.Accounts) {
    const row = document.createElement("div");
    row.className = "account-row";
    row.appendChild(Object.assign(document.createElement("div"), { className: "account-label", textContent: acc.Label }));
    row.appendChild(accountField(acc.ID));
    row.appendChild(accountField(acc.Password));
    body.appendChild(row);
  }
  showModal("accountsModal");
}

function accountField(value) {
  const field = document.createElement("div");
  field.className = "account-field";
  const code = document.createElement("code");
  code.textContent = value;
  field.appendChild(code);
  const copyBtn = actionButton("복사", "btn-sm", true, async () => {
    const origText = copyBtn.textContent;
    try {
      await navigator.clipboard.writeText(value);
      copyBtn.textContent = "복사됨";
    } catch (err) {
      console.error("Failed to copy: ", err);
      copyBtn.textContent = "복사 실패";
    }
    setTimeout(() => { copyBtn.textContent = origText; }, 1500);
  });
  field.appendChild(copyBtn);
  return field;
}

function closeAccountsModal() {
  hideModal("accountsModal");
}

document.getElementById("accountsModalClose").onclick = closeAccountsModal;
document.getElementById("accountsModalBackdrop").onclick = closeAccountsModal;

async function refreshExtStatus() {
  const statusEl = document.getElementById("extStatus");
  if (!statusEl) return;
  try {
    const res = await api("/api/extension-status");
    if (!res.ok) {
      statusEl.textContent = "확인 실패 (런처 재시작 필요)";
      statusEl.style.color = "var(--warning)";
      return;
    }
    const data = await res.json();
    if (data.installed) {
      statusEl.textContent = "설치됨 ✓";
      statusEl.style.color = "var(--success)";
    } else {
      statusEl.textContent = "미설치";
      statusEl.style.color = "var(--muted)";
    }
  } catch (_) {
    statusEl.textContent = "확인 실패";
    statusEl.style.color = "var(--muted)";
  }
}

document.getElementById("installExtBtn").onclick = async () => {
  const btn = document.getElementById("installExtBtn");
  btn.disabled = true;
  btn.textContent = "설치 중…";
  try {
    const res = await api("/api/install-extension", "POST");
    const text = await res.text();
    if (res.ok) {
      alert("확장 설치 완료. VSCode가 열려 있으면 'Developer: Reload Window'로 새로고침하세요.");
      await refreshExtStatus();
    } else {
      alert("설치 실패:\n" + text);
    }
  } finally {
    btn.disabled = false;
    btn.textContent = "설치";
  }
};

document.getElementById("tomcatModalInstall").onclick = async () => {
  const btn = document.getElementById("tomcatModalInstall");
  btn.disabled = true;
  btn.textContent = "설치 중...";
  try {
    const res = await api("/api/install-extension", "POST");
    const text = await res.text();
    if (res.ok) {
      alert("확장 설치 완료. VSCode가 열려 있으면 'Developer: Reload Window'로 새로고침하세요.");
      await refreshExtStatus();
      await updateModalExtState();
    } else {
      alert("설치 실패:\n" + text);
    }
  } finally {
    btn.disabled = false;
    btn.textContent = "설치";
  }
};

async function refresh() {
  const res = await api("/api/targets");
  const views = await res.json();
  render(views);
  renderMonitor(views);
}

// 모니터링: 실제 실행 중인 서비스와 "소스만 준비된" 타깃을 구분해 그룹으로 표시.
// state.port는 미실행 상태에서도 카탈로그 기본 포트로 채워지므로, 실행 중일 때만 포트를 노출한다.
const MONITOR_GROUPS = [
  { title: "실행 중 — 지금 접속 가능", statuses: ["running"] },
  { title: "진행 중 — 클론/빌드 작업", statuses: ["cloning", "building"] },
  { title: "실행 대기 — 소스 준비됨 (Run으로 기동)", statuses: ["cloned", "done", "stopped"] },
  { title: "오류 — 로그 확인 필요", statuses: ["error"] },
];

function renderMonitor(views) {
  const listEl = document.getElementById("monitorList");
  if (!listEl) return;
  const active = views.filter((v) => v.state && v.state.status !== "idle");
  if (active.length === 0) {
    listEl.innerHTML = '<p class="note">클론되었거나 실행 중인 항목이 없습니다.</p>';
    return;
  }
  listEl.innerHTML = "";
  for (const group of MONITOR_GROUPS) {
    const rows = active.filter((v) => group.statuses.includes(v.state.status));
    if (rows.length === 0) continue;

    const title = document.createElement("div");
    title.className = "monitor-group";
    title.textContent = `${group.title} (${rows.length})`;
    listEl.appendChild(title);

    for (const v of rows) {
      const st = v.state.status;
      const port = v.state.port;
      const openable = st === "running" && !!port;
      const row = document.createElement("div");
      row.className = "monitor-row" + (openable ? " openable" : "");
      row.innerHTML =
        `<span class="badge ${st}">${st}</span>` +
        `<span class="mon-name">${v.DisplayName}</span>` +
        `<span class="mon-port">${openable ? ":" + port : "—"}</span>` +
        (openable ? `<span class="mon-open">열기 ↗</span>` : "");
      if (openable) {
        row.title = `http://localhost:${port}${v.state.openPath || v.OpenPath || "/"} 열기`;
        row.onclick = () => window.open(`http://localhost:${port}${v.state.openPath || v.OpenPath || "/"}`);
      }
      listEl.appendChild(row);
    }
  }
}

loadConfig();
refresh();
refreshExtStatus();
setInterval(refresh, 1500);

// Config panel collapse/expand toggling
const configCardEl = document.getElementById("configCard");
const configHeaderEl = document.getElementById("configHeader");
const collapseConfigBtn = document.getElementById("collapseConfigBtn");
const toggleConfigBtn = document.getElementById("toggleConfig");

function setConfigCollapsed(collapsed) {
  if (!configCardEl || !collapseConfigBtn) return;
  if (collapsed) {
    configCardEl.classList.add("collapsed");
    collapseConfigBtn.textContent = "▼";
    if (toggleConfigBtn) toggleConfigBtn.classList.remove("active");
    localStorage.setItem("config_collapsed", "true");
  } else {
    configCardEl.classList.remove("collapsed");
    collapseConfigBtn.textContent = "▲";
    if (toggleConfigBtn) toggleConfigBtn.classList.add("active");
    localStorage.setItem("config_collapsed", "false");
  }
}

function toggleConfigPanel() {
  const isCollapsed = configCardEl ? configCardEl.classList.contains("collapsed") : false;
  setConfigCollapsed(!isCollapsed);
}

if (configHeaderEl) {
  configHeaderEl.onclick = () => {
    toggleConfigPanel();
  };
}

if (toggleConfigBtn) {
  toggleConfigBtn.onclick = () => {
    toggleConfigPanel();
  };
}

// Restore collapsed state on load
if (localStorage.getItem("config_collapsed") === "true") {
  setConfigCollapsed(true);
}

// Guide toggling and OS tab handling
const installGuideEl = document.getElementById("installGuide");
const toggleGuideBtn = document.getElementById("toggleGuide");
const tabMacBtn = document.getElementById("tabMac");
const tabWinBtn = document.getElementById("tabWin");
const guideMacEl = document.getElementById("guideMac");
const guideWinEl = document.getElementById("guideWin");

toggleGuideBtn.onclick = () => {
  installGuideEl.classList.toggle("hidden");
};

const legendEl = document.getElementById("legend");
const toggleLegendBtn = document.getElementById("toggleLegend");
if (toggleLegendBtn && legendEl) {
  toggleLegendBtn.onclick = () => legendEl.classList.toggle("hidden");
}

const monitorEl = document.getElementById("monitor");
const toggleMonitorBtn = document.getElementById("toggleMonitor");
if (toggleMonitorBtn && monitorEl) {
  toggleMonitorBtn.onclick = () => monitorEl.classList.toggle("hidden");
}

tabMacBtn.onclick = () => {
  tabMacBtn.classList.add("active");
  tabWinBtn.classList.remove("active");
  guideMacEl.classList.remove("hidden");
  guideWinEl.classList.add("hidden");
};

tabWinBtn.onclick = () => {
  tabWinBtn.classList.add("active");
  tabMacBtn.classList.remove("active");
  guideWinEl.classList.remove("hidden");
  guideMacEl.classList.add("hidden");
};

// Auto-detect OS on load
if (navigator.platform.toUpperCase().indexOf('WIN') !== -1) {
  tabWinBtn.click();
} else {
  tabMacBtn.click();
}

// 기동 시각 표시 — 재시작이 실제로 반영됐는지 사용자가 바로 확인 (stale 런처 방지)
(async function showVersion() {
  try {
    const res = await api("/api/version");
    if (!res.ok) return;
    const v = await res.json();
    const hdr = document.querySelector(".header-right");
    if (!hdr) return;
    const span = document.createElement("span");
    span.className = "build-stamp";
    span.title = "런처 기동 시각 (재시작하면 갱신됨)";
    span.textContent = `기동 ${v.startedAt} (pid ${v.pid})`;
    hdr.insertBefore(span, hdr.firstChild);
  } catch (_) { /* ignore */ }
})();
