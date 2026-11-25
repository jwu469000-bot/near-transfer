"use strict";

const element = (id) => document.getElementById(id);
const isDesktop = document.body.dataset.side === "desktop";
const ownSide = isDesktop ? "desktop" : "phone";
const pollDelay = 1500;
const queue = [];
const filters = [...document.querySelectorAll("[data-filter]")];
let snapshot;
let ready = false;
let stopped = false;
let pairedHere = false;
let activeUpload;
let requestInFlight;
let selectedNetwork = 0;
let networkKey = "";
let selectedLink;
let filter = "all";
let renderedFiles = "";
let toastTimer;
let confirmWork;

const joinCode = new URLSearchParams(location.hash.slice(1)).get("join");
const timeFormat = new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });

function showMessage(message = "") {
  element("message").textContent = message;
  element("message").hidden = !message;
}

function showToast(message) {
  clearTimeout(toastTimer);
  const toast = element("toast");
  toast.textContent = message;
  toast.hidden = false;
  toastTimer = setTimeout(() => { toast.hidden = true; }, 3400);
}

function setStatus(status, label) {
  element("network").dataset.status = status;
  const target = element("network-label");
  if (target.textContent !== label) target.textContent = label;
}

function showGate(title, message, retry = false) {
  element("workspace").hidden = true;
  element("gate-title").textContent = title;
  element("gate-text").textContent = message;
  element("retry-pair").hidden = !retry;
  element("gate").hidden = false;
}

async function api(path, method = "GET", data) {
  const options = { method, cache: "no-store" };
  if (data) {
    options.body = JSON.stringify(data);
    options.headers = { "Content-Type": "application/json" };
  }
  const response = await fetch(path, options);
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    const error = new Error(body.error || "操作失败，请重试。");
    error.status = response.status;
    throw error;
  }
  return response.status === 204 ? undefined : response.json();
}

function applyState(next) {
  if (snapshot && snapshot.room !== next.room) clearQueue();
  const previous = snapshot;
  snapshot = next;
  ready = true;
  showMessage();
  element("pick-files").disabled = false;
  if (isDesktop) {
    paintPairing();
  } else {
    pairedHere = true;
    setStatus("connected", "已连接电脑");
    element("gate").hidden = true;
    element("workspace").hidden = false;
  }

  if (previous && previous.room === next.room) {
    const received = next.files.slice(previous.files.length).filter((file) => file.sender !== ownSide).length;
    if (received) element("new-files").textContent = "收到 " + received + " 个文件";
  }
  paintHistory();
  sendNext();
}

async function refresh() {
  if (stopped) return;
  if (requestInFlight) return requestInFlight;
  requestInFlight = (async () => {
    try {
      const next = await api("/api/state");
      if (!stopped) applyState(next);
    } catch (error) {
      if (stopped) return;
      ready = false;
      element("pick-files").disabled = true;
      setStatus("offline", "连接中断");
      if (!isDesktop && error.status === 401) {
        pairedHere = false;
        clearQueue();
        showMessage();
        showGate("请扫码连接", "请扫描电脑端当前显示的二维码。");
      } else {
        showMessage("与电脑的连接中断，正在尝试恢复。");
      }
    }
  })();
  try {
    await requestInFlight;
  } finally {
    requestInFlight = undefined;
  }
}

async function poll() {
  if (stopped) return;
  if (isDesktop || pairedHere) await refresh();
  if (!stopped) setTimeout(poll, pollDelay);
}

async function connectPhone() {
  showGate("正在连接电脑", "请保持电脑上的近传程序开启。");
  try {
    if (joinCode) {
      await api("/api/pair", "POST", { code: joinCode });
      history.replaceState(null, "", "/");
    }
    const state = await api("/api/state");
    applyState(state);
  } catch (error) {
    const expired = [401, 403, 409].includes(error.status);
    setStatus("offline", "等待配对");
    showGate(expired ? "请扫码连接" : "暂时无法连接电脑",
      expired ? error.message : "确认两台设备连接同一个 Wi‑Fi 后重试。", !expired);
  }
}

function paintPairing() {
  const { links, paired, bound, online, room, manualCode } = snapshot;
  element("open-installer").hidden = !snapshot.installer;
  const select = element("network-choice");
  const linkKey = JSON.stringify(links.map((link) => link.address));
  if (networkKey !== linkKey) {
    networkKey = linkKey;
    select.replaceChildren(...links.map((link, index) => new Option(link.name + " · " + link.address, index)));
    selectedNetwork = 0;
  }
  selectedLink = links[selectedNetwork];
  const scanning = !paired && !!selectedLink;

  element("unpaired").hidden = !scanning;
  element("paired").hidden = !paired;
  element("no-lan").hidden = scanning || paired;
  element("copy-link").hidden = paired;
  element("copy-link").disabled = !scanning;
  element("reset-pair").hidden = !paired;
  element("pair-heading").textContent = paired ? "设备连接" : "手机扫码连接";
  element("pair-subtitle").textContent = paired
    ? (bound ? "本机已记住这台安卓设备" : "当前浏览器会话可互传文件")
    : "用安卓近传 App 扫码，绑定一次即可";
  element("manual-code").hidden = !scanning;
  element("code-digits").textContent = manualCode ? manualCode.slice(0, 3) + " " + manualCode.slice(3) : "";
  select.hidden = !scanning || links.length < 2;
  select.value = String(selectedNetwork);
  element("pair-address").hidden = !select.hidden;
  element("pair-address").textContent = paired ? "本次会话" : (selectedLink?.address || "没有可用地址");
  const manual = element("manual-link");
  if (manual.value !== (selectedLink?.url || "")) manual.hidden = true;
  manual.value = selectedLink?.url || "";

  if (scanning) {
    const src = "/api/qr?index=" + selectedNetwork + "&room=" + room + "&address=" + encodeURIComponent(selectedLink.address);
    const qr = element("qr");
    if (qr.getAttribute("src") !== src) {
      qr.hidden = true;
      qr.src = src;
    }
  }
  if (paired) {
    element("paired-label").textContent = online ? "手机已连接" : "手机暂离";
    element("paired-detail").textContent = online ? "现在可以双向收发文件" : (bound ? "打开手机近传会自动重连" : "在手机上打开近传即可恢复");
    setStatus(online ? "connected" : "offline", online ? "手机已连接" : "手机暂离");
  } else {
    setStatus(scanning ? "ready" : "offline", scanning ? "等待手机连接" : "未发现局域网");
  }
}

function prettySize(bytes) {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let unit = 0;
  while (bytes >= 1024 && unit < units.length - 1) {
    unit++;
    bytes /= 1024;
  }
  return (unit && bytes < 100 ? bytes.toFixed(1) : Math.round(bytes)) + " " + units[unit];
}

function make(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function icon(name) {
  const node = make("span", "icon i-" + name);
  node.setAttribute("aria-hidden", "true");
  return node;
}

function paintHistory() {
  const all = snapshot?.files || [];
  const files = all.filter((file) => filter === "all" || (file.sender === ownSide ? filter === "out" : filter === "in")).reverse();
  const key = filter + ":" + files.map((file) => file.id).join(",");
  if (renderedFiles === key) return;
  renderedFiles = key;
  const list = element("file-list");
  list.hidden = files.length === 0;
  element("empty").hidden = !!files.length;
  element("empty-title").textContent = { all: "还没有传输记录", in: "还没有收到文件", out: "还没有发出文件" }[filter];
  element("empty-desc").textContent = {
    all: "收发的文件会出现在这里", in: "对方发来的文件会出现在这里", out: "从上方选择文件即可发送",
  }[filter];
  list.replaceChildren(...files.map((file) => {
    const incoming = file.sender !== ownSide;
    const row = make("li", "file-row");
    row.dataset.direction = incoming ? "in" : "out";
    const fileIcon = make("span", "file-icon");
    fileIcon.append(icon("file"));
    const details = make("span", "file-details");
    const name = make("strong", "file-name", file.name);
    name.title = file.name;
    details.append(name, make("span", "file-meta", prettySize(file.size) + " · " + timeFormat.format(new Date(file.createdAt))));
    const save = make("a", "download-button");
    save.href = "/api/files/" + encodeURIComponent(file.id);
    save.download = file.name;
    save.setAttribute("aria-label", "下载 " + file.name);
    save.append(icon("download"), "下载");
    save.addEventListener("click", (event) => {
      if (ready) return;
      event.preventDefault();
      showToast("连接中断，请稍后重试。");
    });
    row.append(fileIcon, details, make("span", "direction", incoming ? "收到" : "发出"), save);
    return row;
  }));
}

function randomID() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (part) => part.toString(16).padStart(2, "0")).join("");
}

function addFiles(files, skippedFolder = false) {
  if (!ready) {
    showToast("请先恢复连接。");
    return;
  }
  if (skippedFolder) showToast("请选择文件。文件夹可以压缩后再发送。");
  for (const file of files) {
    const item = { file, id: randomID(), room: snapshot.room, status: "pending" };
    const row = make("li", "queue-item");
    const details = make("div", "file-details");
    const name = make("strong", "file-name", file.name);
    name.title = file.name;
    const meta = make("span", "file-meta");
    details.append(name, meta);
    const actions = make("div", "actions");
    const retry = make("button", "outline-button", "重试");
    retry.type = "button";
    retry.addEventListener("click", () => {
      if (!ready) {
        showToast("请先恢复连接。");
        return;
      }
      updateItem(item, "pending", "等待发送");
      sendNext();
    });
    const cancel = make("button", "icon-button");
    cancel.type = "button";
    cancel.setAttribute("aria-label", "取消发送 " + file.name);
    cancel.append(icon("close"));
    cancel.addEventListener("click", () => removeItem(item));
    actions.append(retry, cancel);
    const progress = make("progress");
    progress.max = 100;
    progress.value = 0;
    progress.setAttribute("aria-label", file.name + " 的传输进度");
    row.append(details, actions, progress);
    Object.assign(item, { row, progress, meta, retry });
    queue.push(item);
    updateItem(item, "pending", "等待发送");
    element("queue").append(row);
  }
  element("queue").hidden = !queue.length;
  sendNext();
}

function updateItem(item, status, message) {
  item.status = status;
  item.row.dataset.status = status;
  item.meta.textContent = prettySize(item.file.size) + " · " + message;
  item.retry.hidden = status !== "failed";
  item.progress.hidden = status !== "sending";
}

function removeItem(item) {
  const index = queue.indexOf(item);
  if (index < 0) return;
  queue.splice(index, 1);
  item.row.remove();
  element("queue").hidden = !queue.length;
  item.xhr?.abort();
}

function clearQueue() {
  for (const item of [...queue].reverse()) removeItem(item);
}

function sendNext() {
  if (activeUpload || !ready || stopped) return;
  const item = queue.find((candidate) => candidate.status === "pending");
  if (!item) return;
  activeUpload = item;
  item.progress.value = 0;
  updateItem(item, "sending", "发送中");
  const xhr = new XMLHttpRequest();
  item.xhr = xhr;

  function finish(message) {
    item.xhr = undefined;
    activeUpload = undefined;
    if (queue.includes(item)) {
      if (message) updateItem(item, "failed", message);
      else {
        removeItem(item);
        showToast("已发送 " + item.file.name);
        refresh();
      }
    }
    sendNext();
  }

  xhr.upload.addEventListener("progress", (event) => {
    if (!event.lengthComputable || !event.total) return;
    const percent = Math.min(100, Math.floor(100 * event.loaded / event.total));
    item.progress.value = percent;
    updateItem(item, "sending", percent === 100 ? "电脑正在保存" : "已发送 " + percent + "%");
  });
  xhr.addEventListener("load", () => {
    if (xhr.status === 201 || xhr.status === 200) finish();
    else finish(xhr.response?.error || "发送失败，请重试。");
  });
  xhr.addEventListener("error", () => finish("连接中断，可重试此文件。"));
  xhr.addEventListener("abort", () => finish("发送已取消。"));
  xhr.open("POST", "/api/upload");
  xhr.responseType = "json";
  xhr.setRequestHeader("Content-Type", "application/octet-stream");
  xhr.setRequestHeader("X-File-Name", encodeURIComponent(item.file.name));
  xhr.setRequestHeader("X-Transfer-Id", item.id);
  xhr.setRequestHeader("X-Room", String(item.room));
  xhr.send(item.file);
}

element("pick-files").addEventListener("click", () => element("file-picker").click());
element("file-picker").addEventListener("change", (event) => {
  addFiles(event.target.files);
  event.target.value = "";
});
for (const button of filters) {
  button.addEventListener("click", () => {
    filter = button.dataset.filter;
    for (const entry of filters) entry.setAttribute("aria-pressed", String(button === entry));
    paintHistory();
  });
}

const help = element("help");
element("open-help").addEventListener("click", () => help.showModal());

if (isDesktop) {
  const dropzone = element("dropzone");
  let enters = 0;
  const isFileDrag = (event) => event.dataTransfer.types.includes("Files");
  dropzone.addEventListener("dragenter", (event) => {
    if (!isFileDrag(event)) return;
    event.preventDefault();
    enters++;
    dropzone.classList.add("drag-over");
  });
  dropzone.addEventListener("dragleave", () => {
    enters = Math.max(0, enters - 1);
    if (enters === 0) dropzone.classList.remove("drag-over");
  });
  document.addEventListener("dragover", (event) => {
    if (!isFileDrag(event)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = dropzone.contains(event.target) && ready ? "copy" : "none";
  });
  document.addEventListener("drop", (event) => {
    if (!isFileDrag(event)) return;
    event.preventDefault();
    enters = 0;
    dropzone.classList.remove("drag-over");
    if (!dropzone.contains(event.target)) return;
    const items = Array.from(event.dataTransfer.items).filter((item) => item.kind === "file");
    if (!items.length) {
      addFiles(event.dataTransfer.files);
      return;
    }
    const files = items.map((item) => item.webkitGetAsEntry?.()?.isDirectory ? null : item.getAsFile());
    addFiles(files.filter(Boolean), files.includes(null));
  });
  element("qr").addEventListener("load", () => { element("qr").hidden = false; });
  element("open-installer").addEventListener("click", () => {
    const showQR = snapshot.lanCount > 0;
    const image = element("installer-image");
    element("installer-qr").hidden = !showQR;
    element("installer-tip").textContent = showQR
      ? "用手机相机扫码下载。安装后打开近传，再扫描工作台上的配对二维码。"
      : "下载 APK 后安装到手机。电脑接入 Wi‑Fi 后，重新打开近传即可配对。";
    if (showQR) image.src = "/api/qr?install=1&index=" + selectedNetwork;
    element("installer").showModal();
  });
  element("network-choice").addEventListener("change", (event) => {
    selectedNetwork = Number(event.target.value);
    paintPairing();
  });
  element("copy-link").addEventListener("click", async () => {
    if (!selectedLink) return;
    const manual = element("manual-link");
    try {
      await navigator.clipboard.writeText(selectedLink.url);
      showToast("连接链接已复制");
    } catch {
      manual.hidden = false;
      manual.focus();
      manual.select();
      showToast("请复制选中的链接。");
    }
  });

  const confirm = element("confirm");
  function ask(title, message, label, action) {
    if (help.open) help.close();
    element("confirm-title").textContent = title;
    element("confirm-text").textContent = message;
    element("confirm-action").textContent = label;
    confirmWork = action;
    confirm.showModal();
  }
  function askReset() {
    ask(snapshot?.paired ? "解除当前绑定" : "生成新二维码", "当前连接将结束。请先下载要保留的待收文件。", "生成二维码", async () => {
      await api("/api/reset", "POST");
      clearQueue();
      await refresh();
      showToast("已生成新的连接二维码");
    });
  }
  element("reset-pair").addEventListener("click", askReset);
  element("new-connection").addEventListener("click", askReset);
  element("stop-service").addEventListener("click", () => {
    ask("退出近传", "当前传输会结束。请先下载要保留的待收文件。", "退出近传", async () => {
      await api("/api/stop", "POST");
      stopped = true;
      ready = false;
      clearQueue();
      element("workspace").hidden = true;
      setStatus("offline", "传输已结束");
      showMessage("本次传输已结束，可以关闭这个页面。");
    });
  });
  element("confirm-action").addEventListener("click", async (event) => {
    const button = event.currentTarget;
    button.disabled = true;
    try {
      await confirmWork();
      confirm.close();
    } catch (error) {
      showToast(error.message || "操作失败，请重试。");
    } finally {
      button.disabled = false;
    }
  });
} else {
  element("retry-pair").addEventListener("click", connectPhone);
  connectPhone();
}

poll();
