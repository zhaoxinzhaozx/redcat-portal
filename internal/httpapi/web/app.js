const base = document.querySelector('meta[name="portal-base"]').content.replace(/\/$/, "")
const app = document.querySelector("#app")
let site = { name: "站点", slogan: "", contact: "", url: "", downloads: [] }

function token() {
  return localStorage.getItem("auth_token") || ""
}

async function api(path, options) {
  const headers = { "Content-Type": "application/json" }
  if (token()) headers.Authorization = "Bearer " + token()
  const response = await fetch(base + path, Object.assign({ headers }, options || {}))
  const data = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(data.error || "请求失败")
  return data
}

function money(value) {
  return Number(value || 0).toFixed(2)
}

function when(value) {
  return new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    year: "numeric", month: "2-digit", day: "2-digit",
    hour: "2-digit", minute: "2-digit", hourCycle: "h23"
  }).format(new Date(value))
}

function inputTime(value) {
  const parts = new Intl.DateTimeFormat("sv-SE", {
    timeZone: "Asia/Shanghai",
    year: "numeric", month: "2-digit", day: "2-digit",
    hour: "2-digit", minute: "2-digit", hourCycle: "h23"
  }).format(new Date(value))
  return parts.replace(" ", "T")
}

function phaseText(phase) {
  return { running: "进行中", waiting: "等待公布", announced: "已公布", upcoming: "未开始", archived: "历史" }[phase] || phase
}

function route() {
  const path = location.pathname.replace(/\/$/, "")
  if (path === base + "/activities") return { name: "list" }
  if (path === base + "/admin") return { name: "admin" }
  if (path === base + "/admin/new") return { name: "edit", id: 0 }
  const activity = path.match(new RegExp("^" + base + "/activities/(\\d+)$"))
  if (activity) return { name: "detail", id: activity[1] }
  const admin = path.match(new RegExp("^" + base + "/admin/(\\d+)$"))
  if (admin) return { name: "edit", id: admin[1] }
  return { name: "home" }
}

async function refreshNav() {
  const login = document.querySelector("#login-link")
  const admin = document.querySelector("#admin-link")
  if (!token()) {
    login.hidden = false
    admin.hidden = true
    return
  }
  try {
    const me = await api("/api/me")
    login.hidden = true
    admin.hidden = !me.admin
  } catch (error) {
    login.hidden = false
    admin.hidden = true
  }
}

function home() {
  const name = escapeHTML(site.name)
  const address = escapeHTML(site.url || "")
  const host = escapeHTML(String(site.url || "").replace(/^https?:\/\//, ""))
  const contact = site.contact ? `<section class="card"><h2>联系方式</h2><p>${escapeHTML(site.contact)}</p></section>` : ""
  app.innerHTML = `
    <p class="meta">使用教程</p>
    <h1>${name}</h1>
    <p class="slogan">${escapeHTML(site.slogan)}</p>
    <p>按下面的顺序创建密钥。第 5 步下载 CC Switch，第 6 步下载 Codex 或 Claude Code，第 7 步回到密钥列表，点「导入到 CCS」。</p>
    ${step(1, "注册登录", "<p>打开站点，完成注册并登录。</p>")}
    ${step(2, "创建密钥", "<p>在左侧打开「API 密钥」，点击「创建密钥」。</p>")}
    ${step(3, "选择厂商", "<ul><li>Codex 选厂商 OpenAI。</li><li>Claude Code 选 Anthropic。</li><li>Grok 和其他模型，在控制台里选对应厂商。</li></ul>")}
    ${step(4, "选择分组", "<p>OpenAI 分组：</p><ul class='chips'><li>GPT 限时迎新特惠</li><li>GPT 特惠</li><li>GPT Plus 官方</li><li>GPT Pro 官方</li><li>GPT-6.1-sol</li></ul><p>其他厂商的分组以控制台为准。</p>")}
    ${step(5, "下载 CC Switch", `<p>CC Switch 有 Windows 和苹果电脑两个版本。点按钮后先确认，再开始下载。</p>
      ${downloadBlock(5)}
      <div class="grid two"><div class="figure">图 1　Windows 版双击 msi 安装。如果没有反应，右键文件，打开「属性」，勾选「解除锁定」。</div><div class="figure">图 2　苹果电脑打开 dmg，把 CC Switch 拖进「应用程序」。</div></div>`)}
    ${step(6, "下载 Codex / Claude Code", `<p>按配置好的地址下载。这里以 Windows 为例，安装包后缀是 .exe。点按钮后先确认。</p>
      ${downloadBlock(6)}
      <div class="figure">图 3　下载 .exe 后双击安装。</div>`)}
    ${step(7, "用 CC Switch 导入", `<p>回到密钥列表，点这条密钥上的「导入到 CCS」。浏览器询问是否打开 CC Switch 时点「打开」，核对地址 ${host}，确认导入后打开 Codex。</p>`)}
    <section class="finale"><h2>开始尽情享用 Codex / Claude Code</h2><p>恭喜，整个流程已经走完。${name} 已接入，现在可以直接开始使用。</p></section>
    <section class="card"><h2>接口地址</h2><p>${address}</p><p class="row"><a class="button" href="/login" target="_top">登录</a><a class="button ghost" href="${base}/activities">查看抽奖活动</a></p></section>
    ${contact}
  `
  app.querySelectorAll("[data-download]").forEach((button) => {
    button.addEventListener("click", () => askDownload(button))
  })
}

function downloadBlock(step) {
  const items = (site.downloads || []).map((item, index) => ({ item, index })).filter((entry) => Number(entry.item.step) === step)
  if (!items.length) return `<p class="meta">这一步的下载地址还没有配置。</p>`
  return items.map(({ item, index }) => `<p><strong>${escapeHTML(item.label)}</strong></p><p><button type="button" data-download="${index}">${escapeHTML(item.button)}</button></p>`).join("")
}

function step(n, title, body) {
  return `<section class="step"><span class="num">${n}</span><div><h2>${title}</h2>${body}</div></section>`
}

function askDownload(button) {
  const item = (site.downloads || [])[Number(button.dataset.download)]
  if (!item) return
  document.querySelectorAll(".confirm").forEach((node) => node.remove())
  const box = document.createElement("div")
  box.className = "card confirm"
  box.innerHTML = `<p>确定要下载 ${escapeHTML(item.label || item.button)} 吗？</p><div class="row"><button type="button" id="yes">确定下载</button><button type="button" class="ghost" id="no">取消</button></div>`
  const anchor = button.closest("p") || button
  anchor.insertAdjacentElement("afterend", box)
  box.querySelector("#yes").onclick = () => { location.assign(item.url) }
  box.querySelector("#no").onclick = () => box.remove()
}

async function activityList() {
  app.innerHTML = "<h1>抽奖活动</h1><p class='meta'>这里只显示当前可以查看的活动。</p>"
  let data
  try { data = await api("/api/activities") } catch (error) { app.insertAdjacentHTML("beforeend", `<p>${error.message}</p>`); return }
  if (!data.activities.length) {
    app.insertAdjacentHTML("beforeend", `<section class="empty"><p class="meta">当前时段</p><h2>暂无活动</h2><p>有新的活动开始后，会出现在这里。</p></section>`)
    return
  }
  for (const item of data.activities) {
    const card = document.createElement("a")
    card.className = "card"
    card.href = `${base}/activities/${item.id}`
    card.innerHTML = `<p class="badge">${phaseText(item.phase)}</p><h2>${escapeHTML(item.title)}</h2><p>参与人数 ${item.participant_count}</p><p class="meta">奖励 ${money(item.reward_amount)} 元 · 公布 ${when(item.announce_at)}</p>`
    app.appendChild(card)
  }
}

async function activityDetail(id) {
  app.innerHTML = "<p class='meta'>正在读取活动</p>"
  let item
  try { item = await api("/api/activities/" + id) } catch (error) {
    app.innerHTML = `<section class="empty"><h2>活动不存在</h2><p><a href="${base}/activities">返回活动列表</a></p></section>`
    return
  }
  const actions = document.createElement("div")
  actions.className = "row"
  if (!token()) actions.innerHTML = `<a class="button" href="/login" target="_top">登录后参与</a>`
  else if (item.phase === "running" && item.joined) actions.innerHTML = `<button class="ghost" disabled>已参与</button>`
  else if (item.phase === "running" && item.eligible === false) actions.innerHTML = `<p>您暂时无法参与此活动</p><p class="meta">当前累计实付充值 ${money(item.recharged_amount)} 元，本场需要 ${money(item.recharge_threshold)} 元。</p>`
  else if (item.phase === "running") {
    actions.innerHTML = `<button id="join">参与活动</button>`
  } else if (item.phase === "waiting") actions.innerHTML = `<p>参与已截止，等待公布中奖用户。</p>`
  else if (item.can_claim) actions.innerHTML = `<button id="claim">立即领取</button>`
  else if (item.claimed) actions.innerHTML = `<button class="ghost" disabled>已领取</button>`

  let winner = ""
  if (item.phase === "announced") {
    winner = item.has_winner
      ? `<section class="card"><h2>中奖用户</h2><ul>${(item.winners || []).map((email) => `<li>${escapeHTML(email)}</li>`).join("")}</ul></section>`
      : `<p class="winner">本次活动没有产生中奖用户</p>`
  }
  app.innerHTML = `
    <p class="meta"><a href="${base}/activities">活动列表</a> · ${phaseText(item.phase)}</p>
    <h1>${escapeHTML(item.title)}</h1>
    <p>${escapeHTML(item.body)}</p>
    <section class="card"><p class="meta">参与人数</p><p class="count">${item.participant_count}</p></section>
    <section class="card"><p>奖励金额 ${money(item.reward_amount)} 元</p><p>充值满 ${money(item.recharge_threshold)} 元可以参与</p><p class="meta">开始 ${when(item.start_at)} · 结束 ${when(item.end_at)} · 公布 ${when(item.announce_at)}</p></section>
    ${winner}
  `
  app.appendChild(actions)
  const join = document.querySelector("#join")
  if (join) join.onclick = async () => {
    join.disabled = true
    try { await api(`/api/activities/${id}/join`, { method: "POST" }); activityDetail(id) }
    catch (error) { join.disabled = false; actions.insertAdjacentHTML("beforeend", `<p>${escapeHTML(error.message)}</p>`) }
  }
  const claim = document.querySelector("#claim")
  if (claim) claim.onclick = async () => {
    claim.disabled = true
    try { await api(`/api/activities/${id}/claim`, { method: "POST" }); activityDetail(id) }
    catch (error) { claim.disabled = false; actions.insertAdjacentHTML("beforeend", `<p>${escapeHTML(error.message)}</p>`) }
  }
}

async function adminList() {
  app.innerHTML = `<div class="row"><h1>全部活动</h1><a class="button" href="${base}/admin/new">新建活动</a></div>`
  let data
  try { data = await api("/api/admin/activities") } catch (error) {
    app.insertAdjacentHTML("beforeend", `<p>${escapeHTML(error.message)}</p>`)
    return
  }
  if (!data.activities.length) {
    app.insertAdjacentHTML("beforeend", `<section class="empty"><h2>还没有活动</h2></section>`)
    return
  }
  for (const item of data.activities) {
    const card = document.createElement("a")
    card.className = "card"
    card.href = `${base}/admin/${item.id}`
    card.innerHTML = `<p class="badge">${phaseText(item.phase)}${item.archived ? " · 历史" : ""}</p><h2>${escapeHTML(item.title)}</h2><p class="meta">展示人数 ${item.participant_count} · 真实参与 ${item.real_count}</p>`
    app.appendChild(card)
  }
}

async function adminEdit(id) {
  const blank = { title: "", body: "", reward_amount: 10, recharge_threshold: 50, winner_count: 1, start_at: "", end_at: "", announce_at: "", participants: [], selected_user_ids: [], winners: [], drawn: false, archived: false, participant_count: 0, real_count: 0 }
  let item = blank
  if (id) {
    try { item = await api("/api/admin/activities/" + id) } catch (error) {
      app.innerHTML = `<p>${escapeHTML(error.message)}</p>`
      return
    }
    item.start_at = inputTime(item.start_at)
    item.end_at = inputTime(item.end_at)
    item.announce_at = inputTime(item.announce_at)
  }
  const selected = new Set((item.selected_user_ids || []).map(Number))
  const picks = (item.participants || []).map((person) => `<label class="check"><input type="checkbox" name="winner" value="${person.user_id}" ${selected.has(Number(person.user_id)) ? "checked" : ""} ${item.drawn ? "disabled" : ""}> ${escapeHTML(person.email)}</label>`).join("")
  const drawnNames = (item.winners || []).map((winner) => `${escapeHTML(winner.email)}${winner.claimed ? "（已领取）" : ""}`).join("、")
  app.innerHTML = `
    <p class="meta"><a href="${base}/admin">全部活动</a></p>
    <h1>${id ? "编辑活动" : "新建活动"}</h1>
    <form id="form" class="card">
      <label>活动名称</label><input name="title" required value="${escapeAttr(item.title)}" ${item.drawn ? "disabled" : ""}>
      <label>用户看到的文字</label><textarea name="body" ${item.drawn ? "disabled" : ""}>${escapeHTML(item.body)}</textarea>
      <label>奖励金额</label><input name="reward_amount" type="number" min="0.01" step="0.01" required value="${item.reward_amount}" ${item.drawn ? "disabled" : ""}>
      <label>充值满多少可以参与</label><input name="recharge_threshold" type="number" min="0" step="0.01" required value="${item.recharge_threshold}" ${item.drawn ? "disabled" : ""}>
      <label>中奖人数</label><input name="winner_count" type="number" min="1" step="1" required value="${item.winner_count || 1}" ${item.drawn ? "disabled" : ""}>
      <label>开始时间</label><input name="start_at" type="datetime-local" required value="${item.start_at}" ${item.drawn ? "disabled" : ""}>
      <label>结束时间</label><input name="end_at" type="datetime-local" required value="${item.end_at}" ${item.drawn ? "disabled" : ""}>
      <label>公布时间</label><input name="announce_at" type="datetime-local" required value="${item.announce_at}" ${item.drawn ? "disabled" : ""}>
      ${item.drawn ? "" : `<p class="row"><button>保存</button></p>`}
      <p id="form-error"></p>
    </form>
    ${id ? `<section class="card"><p class="meta">参与人数</p><p class="count" id="count">${item.participant_count}</p><p class="meta">真实参与 ${item.real_count} 人。加号只增加展示人数。</p><button id="plus" class="plus" ${item.archived ? "disabled" : ""}>+</button></section>
      <section class="card"><label>指定中奖用户</label><div class="picks">${picks || "<p class='meta'>还没有人参与，暂时不能指定。</p>"}</div><button id="save-winner" class="ghost" ${item.drawn ? "disabled" : ""}>保存选择</button><p class="meta">${drawnNames ? "已开奖：" + drawnNames : "最多勾选中奖人数这么多个。没选满时，公布时从其余真实参与者里随机补足。修改中奖人数后要先保存活动。"}</p></section>
      <section class="card"><button id="archive" class="ghost">归入历史</button><p class="meta">归入历史后，普通用户不再看到这场活动。</p></section>` : ""}
  `
  document.querySelector("#form").onsubmit = async (event) => {
    event.preventDefault()
    const form = new FormData(event.target)
    const payload = Object.fromEntries(form.entries())
    payload.reward_amount = Number(payload.reward_amount)
    payload.recharge_threshold = Number(payload.recharge_threshold)
    payload.winner_count = Number(payload.winner_count)
    try {
      const saved = await api(id ? "/api/admin/activities/" + id : "/api/admin/activities", { method: "POST", body: JSON.stringify(payload) })
      location.href = `${base}/admin/${saved.id}`
    } catch (error) {
      document.querySelector("#form-error").textContent = error.message
    }
  }
  const plus = document.querySelector("#plus")
  if (plus) plus.onclick = async () => {
    const result = await api(`/api/admin/activities/${id}/bump`, { method: "POST" })
    document.querySelector("#count").textContent = result.participant_count
  }
  const saveWinner = document.querySelector("#save-winner")
  if (saveWinner) saveWinner.onclick = async () => {
    try {
      const userIDs = [...document.querySelectorAll("input[name=winner]:checked")].map((input) => Number(input.value))
      const limit = Number(item.winner_count || 1)
      if (userIDs.length > limit) throw new Error("选择人数不能超过中奖人数")
      await api(`/api/admin/activities/${id}/winner`, { method: "POST", body: JSON.stringify({ user_ids: userIDs }) })
      saveWinner.textContent = "已保存"
    } catch (error) { saveWinner.textContent = error.message }
  }
  const archive = document.querySelector("#archive")
  if (archive) archive.onclick = async () => {
    await api(`/api/admin/activities/${id}/archive`, { method: "POST" })
    location.href = base + "/admin"
  }
}

function escapeHTML(value) {
  return String(value || "").replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[char]))
}

function escapeAttr(value) {
  return escapeHTML(value)
}

async function render() {
  if (window.self !== window.top) document.body.classList.add("embedded")
  try {
    const loaded = await fetch(base + "/api/site").then((response) => response.json())
    site = { ...site, ...loaded }
  } catch (error) { /* 配置读取失败时使用默认名称 */ }
  document.title = site.name
  const brand = document.querySelector(".brand")
  if (brand) brand.textContent = site.name
  await refreshNav()
  const current = route()
  if (current.name === "home") home()
  else if (current.name === "list") activityList()
  else if (current.name === "detail") activityDetail(current.id)
  else if (current.name === "admin") adminList()
  else adminEdit(current.id)
}

render()
