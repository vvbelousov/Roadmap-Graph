<script lang="ts">
  import { onMount, tick } from "svelte";
  import { en, ru } from "./i18n";
  import {
    chain,
    edgePath,
    timelineScale,
    utc,
    localConflicts,
    type Project,
    type Milestone,
    type Dependency,
    type Graph,
    type Conflict,
  } from "./lib/graph";
  let lang = $state(localStorage.getItem("rg.lang") || "en"),
    theme = $state(localStorage.getItem("rg.theme") || "dark");
  let t = $derived(lang === "ru" ? ru : en);
  const tr = (key: string) => (t as Record<string, string>)[key] || key;
  let graph = $state<Graph>({ projects: [], milestones: [], dependencies: [] }),
    conflicts = $state<Conflict[]>([]),
    csrf = "",
    authenticated = $state(false),
    loading = $state(true),
    isDemo = $state(false),
    password = $state(""),
    error = $state("");
  let hidden = $state<string[]>([]),
    status = $state(""),
    kind = $state(""),
    query = $state(""),
    showEdges = $state(true),
    showConflicts = $state(true),
    onlyConflicts = $state(false),
    page = $state("overview"),
    sidebar = $state(false);
  let selected = $state(""),
    hovered = $state(""),
    tooltip = $state<{ m: Milestone; x: number; y: number } | null>(null),
    modal = $state(""),
    editID = $state(""),
    form = $state<Record<string, any>>({});
  let zoom = $state(localStorage.getItem("rg.zoom") || "year"),
    rangeStart = $state(""),
    rangeEnd = $state(""),
    scroll = $state(0),
    viewWidth = $state(900),
    scroller = $state<HTMLDivElement>(null!),
    dialog = $state<HTMLDialogElement>(null!);
  let whatDate = $state(""),
    simulation = $state(false),
    impact = $state<{ direct: string[]; transitive: string[] }>({
      direct: [],
      transitive: [],
    });
  const statuses = [
      "planned",
      "in_progress",
      "completed",
      "delayed",
      "cancelled",
    ],
    types = [
      "release",
      "feature",
      "integration",
      "market_launch",
      "end_of_life",
      "migration",
      "custom",
    ];
  const date = (s: string) =>
    s
      ? new Intl.DateTimeFormat(lang === "ru" ? "ru-RU" : "en-GB", {
          day: "numeric",
          month: "short",
          year: "numeric",
          timeZone: "UTC",
        }).format(utc(s))
      : "—";
  let projects = $derived(
    [...graph.projects].sort((a, b) => a.sort_order - b.sort_order),
  );
  let visibleProjects = $derived(
    projects.filter((p) => !hidden.includes(p.id)),
  );
  let conflictIDs = $derived(
    new Set(conflicts.map((c) => c.dependent_milestone_id)),
  );
  let milestones = $derived(
    graph.milestones.filter(
      (m) =>
        visibleProjects.some((p) => p.id === m.project_id) &&
        (!status || m.status === status) &&
        (!kind || m.milestone_type === kind) &&
        (!query ||
          `${m.title} ${m.version} ${m.description}`
            .toLowerCase()
            .includes(query.toLowerCase())) &&
        (!onlyConflicts || conflictIDs.has(m.id)),
    ),
  );
  let byID = $derived(new Map(graph.milestones.map((m) => [m.id, m]))),
    projectMap = $derived(new Map(projects.map((p) => [p.id, p])));
  let active = $derived(hovered || selected),
    connected = $derived(
      active ? chain(active, graph.dependencies) : new Set<string>(),
    ),
    current = $derived(byID.get(selected));
  let dates = $derived(graph.milestones.map((m) => m.planned_date).sort()),
    start = $derived(
      rangeStart && (!rangeEnd || rangeStart < rangeEnd)
        ? rangeStart
        : dates[0]
          ? `${Number(dates[0].slice(0, 4))}-${dates[0].slice(5, 7)}-01`
          : `${new Date().getFullYear()}-01-01`,
    );
  let end = $derived(
    rangeEnd && (!rangeStart || rangeEnd > rangeStart)
      ? rangeEnd
      : dates.length
        ? `${Number(dates[dates.length - 1].slice(0, 4)) + 1}-03-01`
        : `${new Date().getFullYear() + 1}-03-01`,
  );
  let width = $derived(
    Math.max(
      viewWidth,
      Math.round(
        ((utc(end).getTime() - utc(start).getTime()) / 86400000) *
          ({ month: 28, quarter: 12, year: 4, multi: 1.8, fit: 0 }[zoom] ?? 4),
      ) + 180,
    ),
  );
  let scale = $derived(timelineScale(start, end, width)),
    height = $derived(Math.max(440, visibleProjects.length * 118 + 90));
  let ticks = $derived(scale.ticks(Math.max(4, Math.floor(width / 110))));
  let rendered = $derived(
    new Map(
      milestones.map((m) => [
        m.id,
        {
          x: scale(utc(m.planned_date)),
          y: 68 + visibleProjects.findIndex((p) => p.id === m.project_id) * 118,
        },
      ]),
    ),
  );
  let edges = $derived(
    graph.dependencies.filter(
      (e) =>
        rendered.has(e.prerequisite_milestone_id) &&
        rendered.has(e.dependent_milestone_id),
    ),
  );
  let simConflicts = $derived(
    current && simulation
      ? localConflicts(
          graph.milestones.map((m) =>
            m.id === current.id ? { ...m, planned_date: whatDate } : m,
          ),
          graph.dependencies,
        )
      : [],
  );
  let newConflicts = $derived(
    simConflicts.filter(
      (c) =>
        !conflicts.some(
          (o) =>
            o.prerequisite_milestone_id === c.prerequisite_milestone_id &&
            o.dependent_milestone_id === c.dependent_milestone_id,
        ),
    ),
  );
  $effect(() => {
    document.documentElement.dataset.theme = theme;
    document.documentElement.lang = lang;
    localStorage.setItem("rg.theme", theme);
    localStorage.setItem("rg.lang", lang);
    localStorage.setItem("rg.zoom", zoom);
  });
  async function api(path: string, method = "GET", body?: any) {
    const res = await fetch("/api" + path, {
      method,
      headers: {
        "Content-Type": "application/json",
        ...(csrf ? { "X-CSRF-Token": csrf } : {}),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const data = res.status === 204 ? null : await res.json();
    if (!res.ok) {
      if (res.status === 401) authenticated = false;
      throw Error(data?.error?.message || `${res.status}`);
    }
    return data;
  }
  async function load() {
    try {
      [graph, conflicts] = await Promise.all([
        api("/graph"),
        api("/analysis/conflicts"),
      ]);
      const settings = await api("/settings");
      isDemo = settings.demo === "fictional";
    } catch (e) {
      error = String((e as Error).message);
    }
  }
  async function session() {
    try {
      const s = await api("/auth/session");
      authenticated = s.authenticated;
      csrf = s.csrf_token || "";
      if (authenticated) await load();
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  }
  onMount(() => {
    session();
    const handler = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "k") {
        e.preventDefault();
        document.querySelector<HTMLInputElement>(".top-search input")?.focus();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  });
  async function login() {
    error = "";
    try {
      const s = await api("/auth/login", "POST", { password });
      csrf = s.csrf_token;
      authenticated = true;
      password = "";
      await load();
    } catch (e) {
      error = (e as Error).message;
    }
  }
  async function logout() {
    try {
      await api("/auth/logout", "POST");
      authenticated = false;
      csrf = "";
    } catch (e) {
      error = (e as Error).message;
    }
  }
  async function choose(m: Milestone) {
    selected = m.id;
    simulation = false;
    whatDate = m.planned_date;
    try {
      impact = await api(`/milestones/${m.id}/impact`);
    } catch {
      impact = { direct: [], transitive: [] };
    }
  }
  function toggleProject(id: string) {
    hidden = hidden.includes(id)
      ? hidden.filter((x) => x !== id)
      : [...hidden, id];
  }
  function reset() {
    hidden = [];
    status = "";
    kind = "";
    query = "";
    rangeStart = "";
    rangeEnd = "";
    onlyConflicts = false;
    showEdges = true;
    showConflicts = true;
  }
  async function open(type: string, id = "") {
    modal = type;
    editID = id;
    error = "";
    if (type === "project")
      form = id
        ? { ...projectMap.get(id) }
        : {
            name: "",
            slug: "",
            description: "",
            color: "#5d89ff",
            icon: "",
            sort_order: projects.length,
          };
    if (type === "milestone")
      form = id
        ? { ...byID.get(id) }
        : {
            project_id: visibleProjects[0]?.id || projects[0]?.id || "",
            title: "",
            description: "",
            milestone_type: "release",
            version: "",
            planned_date: new Date().toISOString().slice(0, 10),
            completed_date: "",
            status: "planned",
            owner: "",
            external_url: "",
          };
    if (type === "dependency")
      form = {
        prerequisite_milestone_id:
          graph.milestones.find((m) => m.id !== selected)?.id || "",
        dependent_milestone_id: selected || graph.milestones[1]?.id || "",
        dependency_type: "requires",
      };
    await tick();
    dialog.showModal();
  }
  function close() {
    dialog.close();
    modal = "";
    error = "";
  }
  async function save() {
    try {
      if (modal === "project" && !form.slug)
        form.slug =
          form.name
            .toLowerCase()
            .replace(/[^a-z0-9]+/g, "-")
            .replace(/^-|-$/g, "") || "project-" + Date.now().toString(36);
      const payload = { ...form };
      delete payload.id;
      delete payload.created_at;
      delete payload.updated_at;
      await api(
        `/${modal === "project" ? "projects" : modal === "milestone" ? "milestones" : "dependencies"}${editID ? "/" + editID : ""}`,
        editID ? "PATCH" : "POST",
        payload,
      );
      close();
      await load();
    } catch (e) {
      error = (e as Error).message;
    }
  }
  async function remove(type: string, id: string) {
    if (!confirm(t.confirmDelete)) return;
    try {
      await api(`/${type}/${id}`, "DELETE");
      if (type === "milestones" && selected === id) selected = "";
      await load();
    } catch (e) {
      error = (e as Error).message;
    }
  }
  async function reorder(p: Project, delta: number) {
    const index = projects.findIndex((x) => x.id === p.id),
      other = projects[index + delta];
    if (!other) return;
    try {
      const ids = projects.map((x) => x.id);
      [ids[index], ids[index + delta]] = [ids[index + delta], ids[index]];
      await api("/projects/order", "PUT", { ids });
      await load();
    } catch (e) {
      error = (e as Error).message;
    }
  }
  async function demo() {
    try {
      await api("/demo", "POST", {});
      await load();
    } catch (e) {
      error = (e as Error).message;
    }
  }
  function today() {
    if (scroller) scroller.scrollLeft = scale(new Date()) - viewWidth / 2;
  }
  function mini(e: MouseEvent) {
    const el = e.currentTarget as HTMLElement;
    scroller.scrollLeft =
      ((e.clientX - el.getBoundingClientRect().left) / el.clientWidth) * width -
      viewWidth / 2;
  }
  async function apply() {
    if (!current) return;
    try {
      await api(`/milestones/${current.id}`, "PATCH", {
        planned_date: whatDate,
      });
      simulation = false;
      await load();
      await choose(byID.get(selected)!);
    } catch (e) {
      error = (e as Error).message;
    }
  }
  const safeURL = (s: string) => (/^https?:\/\//i.test(s) ? s : "");
</script>

{#if loading}<main class="auth">
    <div class="brand"><span class="logo">⌘</span> Roadmap Graph</div>
    <p>{t.load}</p>
  </main>
{:else if !authenticated}<main class="auth">
    <div class="auth-card">
      <div class="brand"><span class="logo">⌘</span> Roadmap Graph</div>
      <h1>{t.login}</h1>
      <p>{t.loginHint}</p>
      <form
        onsubmit={(e) => {
          e.preventDefault();
          login();
        }}
      >
        <label
          >{t.password}<input
            type="password"
            bind:value={password}
            required
            autocomplete="current-password"
          /></label
        >{#if error}<div class="error" role="alert">{error}</div>{/if}<button
          class="primary"
          type="submit">{t.login} →</button
        >
      </form>
      <footer>{t.tagline}</footer>
    </div>
  </main>
{:else}
  <header class="topbar">
    <button
      class="mobile"
      onclick={() => (sidebar = !sidebar)}
      aria-label={t.menu}>☰</button
    ><a
      class="brand"
      href="/"
      onclick={(e) => {
        e.preventDefault();
        page = "overview";
      }}><span class="logo">⌘</span> Roadmap Graph</a
    >
    <nav>
      <button
        class:active={page === "overview"}
        onclick={() => (page = "overview")}>▧ <span>{t.overview}</span></button
      ><button
        class:active={page === "projects"}
        onclick={() => (page = "projects")}>▱ <span>{t.projects}</span></button
      >
    </nav>
    <div class="top-search">
      ⌕ <input
        aria-label={t.search}
        placeholder={t.search}
        bind:value={query}
        oninput={() => (page = "overview")}
      /><kbd>⌘ K</kbd>
    </div>
    <button
      class="language-toggle"
      aria-label={t.language}
      onclick={() => (lang = lang === "en" ? "ru" : "en")}
      >{lang.toUpperCase()}</button
    ><button
      title={t.theme}
      aria-label={t.theme}
      onclick={() => (theme = theme === "dark" ? "light" : "dark")}
      >{theme === "dark" ? "☼" : "☾"}</button
    ><button
      aria-label={t.settings}
      class:active={page === "settings"}
      onclick={() => (page = "settings")}>⚙</button
    >
    <div class="avatar">RG</div>
  </header>
  <div class="app-layout">
    <aside class:open={sidebar} class="sidebar">
      <div class="sidebar-title">
        <span>{t.projects}</span><button
          aria-label={t.addProject}
          onclick={() => open("project")}>＋</button
        >
      </div>
      <div class="project-list">
        {#each projects as p}<div class="project-row">
            <label
              ><input
                type="checkbox"
                checked={!hidden.includes(p.id)}
                onchange={() => toggleProject(p.id)}
              /><i style:background={p.color}></i>{p.name}</label
            ><button
              class="small"
              title={t.editProject}
              aria-label={`${t.editProject} ${p.name}`}
              onclick={() => open("project", p.id)}>···</button
            >
          </div>{/each}{#if !projects.length}<p class="muted">
            {t.emptyProjects}
          </p>{/if}
      </div>
      <div class="sidebar-title filter-title">
        <span>{t.filters}</span><button class="link" onclick={reset}
          >{t.reset}</button
        >
      </div>
      <span class="field-label">{t.dateRange}</span>
      <div class="date-filter">
        <input type="date" aria-label={t.from} bind:value={rangeStart} /><input
          type="date"
          aria-label={t.to}
          bind:value={rangeEnd}
        />
      </div>
      <label class="field-label" for="status-filter">{t.status}</label><select
        id="status-filter"
        bind:value={status}
        ><option value="">{t.all}</option>{#each statuses as s}<option value={s}
            >{tr(s)}</option
          >{/each}</select
      ><label class="field-label" for="type-filter">{t.type}</label><select
        id="type-filter"
        bind:value={kind}
        ><option value="">{t.all}</option>{#each types as s}<option value={s}
            >{tr(s)}</option
          >{/each}</select
      ><span class="field-label">{t.show}</span><label class="check"
        ><input
          type="checkbox"
          bind:checked={showEdges}
        />{t.dependencies}</label
      ><label class="check"
        ><input
          type="checkbox"
          bind:checked={showConflicts}
        />{t.conflicts}</label
      ><label class="check"
        ><input
          type="checkbox"
          bind:checked={onlyConflicts}
        />{t.conflictOnly}</label
      >
      <div class="sidebar-bottom">
        <span class="live-dot"></span> Roadmap Graph
        <span class="version">v0.1</span>
      </div>
    </aside>
    <main class="workspace">
      {#if error && !modal}<div class="error" role="alert">
          {error}<button onclick={() => (error = "")}>×</button>
        </div>{/if}
      {#if page === "settings"}<section class="settings">
          <p class="eyebrow">ROADMAP GRAPH</p>
          <h1>{t.settings}</h1>
          <label
            >{t.language}<select bind:value={lang}
              ><option value="en">English</option><option value="ru"
                >Русский</option
              ></select
            ></label
          ><label
            >{t.theme}<select bind:value={theme}
              ><option value="dark">{t.dark}</option><option value="light"
                >{t.light}</option
              ></select
            ></label
          ><button onclick={logout}>{t.logout}</button>
        </section>
      {:else if page === "projects"}<div class="page-heading">
          <div>
            <p class="eyebrow">{t.workspace}</p>
            <h1>{t.projects}</h1>
          </div>
          <button class="primary" onclick={() => open("project")}
            >＋ {t.addProject}</button
          >
        </div>
        <div class="project-cards">
          {#each projects as p, i}<article
              class="project-card"
              style:--project={p.color}
            >
              <div class="project-symbol">{p.icon || "◇"}</div>
              <h2>{p.name}</h2>
              <p>{p.description}</p>
              <small
                >{graph.milestones.filter((m) => m.project_id === p.id).length}
                {t.releases}</small
              >
              <div class="card-actions">
                <button onclick={() => open("project", p.id)}>{t.edit}</button
                ><button
                  aria-label={`${t.moveUp} ${p.name}`}
                  disabled={i === 0}
                  onclick={() => reorder(p, -1)}>↑</button
                ><button
                  aria-label={`${t.moveDown} ${p.name}`}
                  disabled={i === projects.length - 1}
                  onclick={() => reorder(p, 1)}>↓</button
                ><button class="danger" onclick={() => remove("projects", p.id)}
                  >{t.delete}</button
                >
              </div>
            </article>{/each}
        </div>
      {:else}
        <div class="page-heading">
          <div>
            <p class="eyebrow">{t.workspace} / {t.overview}</p>
            <h1>
              {t.portfolio}<span class="badge"
                >{projects.length} {t.projects.toLowerCase()}</span
              >
            </h1>
            <p class="subtitle">{t.tagline}</p>
          </div>
          <button
            class="primary"
            disabled={!projects.length}
            onclick={() => open("milestone")}>＋ {t.addMilestone}</button
          >
        </div>
        <div class="stats">
          <div>
            <span class="stat-icon blue">▱</span><span class="stat-number"
              >{projects.length}</span
            ><span>{t.active}</span>
          </div>
          <div>
            <span class="stat-icon green">◇</span><span class="stat-number"
              >{graph.milestones.length}</span
            ><span>{t.releases}</span>
          </div>
          <div>
            <span class="stat-icon purple">⌁</span><span class="stat-number"
              >{graph.dependencies.length}</span
            ><span>{t.dependencies}</span>
          </div>
          <button
            class="stat-conflict"
            onclick={() => (onlyConflicts = !onlyConflicts)}
            ><span class="stat-icon amber">△</span><span class="stat-number"
              >{conflicts.length}</span
            ><span>{t.attention}</span></button
          >
        </div>
        {#if !projects.length}<section class="empty">
            <div class="empty-graph">⌁</div>
            <h2>{t.empty}</h2>
            <p>{t.emptyBody}</p>
            <button class="primary" onclick={() => open("project")}
              >＋ {t.addProject}</button
            ><button onclick={demo}>{t.demo}</button>
          </section>
        {:else}<section class="graph-panel">
            <div class="graph-heading">
              <h2>
                <span class="live-dot"></span>{t.roadmap}{#if isDemo}<span
                    class="demo-badge">{t.fictional}</span
                  >{/if}
              </h2>
              <div class="graph-tools">
                <button
                  class:active={zoom === "fit"}
                  onclick={() => (zoom = "fit")}>{t.fit}</button
                ><button onclick={today}>{t.today}</button><span
                  class="tool-divider"
                ></span><button
                  aria-label={t.zoomOut}
                  onclick={() =>
                    (zoom = (
                      {
                        month: "quarter",
                        quarter: "year",
                        year: "multi",
                        multi: "multi",
                        fit: "multi",
                      } as Record<string, string>
                    )[zoom])}>−</button
                ><select aria-label={t.view} bind:value={zoom}
                  ><option value="month">{t.month}</option><option
                    value="quarter">{t.quarter}</option
                  ><option value="year">{t.year}</option><option value="multi"
                    >{t.multi}</option
                  ><option value="fit">{t.fit}</option></select
                ><button
                  aria-label={t.zoomIn}
                  onclick={() =>
                    (zoom = (
                      {
                        multi: "year",
                        year: "quarter",
                        quarter: "month",
                        month: "month",
                        fit: "year",
                      } as Record<string, string>
                    )[zoom])}>＋</button
                >
              </div>
            </div>
            <div class="graph-body">
              <div class="lane-labels">
                {#each visibleProjects as p}<div
                    class="lane-label"
                    style:--project={p.color}
                  >
                    <div class="project-symbol">{p.icon || "◇"}</div>
                    <div>
                      <strong>{p.name}</strong><small>{p.description}</small>
                    </div>
                  </div>{/each}
              </div>
              <div
                class="timeline-scroll"
                bind:this={scroller}
                bind:clientWidth={viewWidth}
                onscroll={() => (scroll = scroller.scrollLeft)}
                onwheel={(e) => {
                  if (e.shiftKey || Math.abs(e.deltaX) > Math.abs(e.deltaY))
                    return;
                  if (e.ctrlKey) {
                    e.preventDefault();
                    zoom = e.deltaY > 0 ? "multi" : "quarter";
                  } else {
                    e.preventDefault();
                    scroller.scrollLeft += e.deltaY;
                  }
                }}
                role="region"
                aria-label={t.timeline}
              >
                <svg {width} {height} class="timeline" aria-label={t.roadmap}
                  ><defs
                    ><marker
                      id="arrow"
                      viewBox="0 0 10 10"
                      refX="9"
                      refY="5"
                      markerWidth="6"
                      markerHeight="6"
                      orient="auto-start-reverse"
                      ><path
                        d="M 0 0 L 10 5 L 0 10 z"
                        fill="context-stroke"
                      /></marker
                    ></defs
                  >
                  {#each ticks as d}<line
                      x1={scale(d)}
                      y1="0"
                      x2={scale(d)}
                      y2={height - 45}
                      class="grid"
                    /><text
                      x={scale(d)}
                      y={height - 20}
                      text-anchor="middle"
                      class="tick"
                      >{new Intl.DateTimeFormat(
                        lang === "ru" ? "ru-RU" : "en-GB",
                        { month: "short", year: "numeric", timeZone: "UTC" },
                      ).format(d)}</text
                    >{/each}
                  {#each visibleProjects as p, i}<rect
                      x="0"
                      y={12 + i * 118}
                      {width}
                      height="108"
                      fill={p.color}
                      opacity=".035"
                      rx="8"
                    /><line
                      x1="0"
                      y1={68 + i * 118}
                      x2={width}
                      y2={68 + i * 118}
                      stroke={p.color}
                      stroke-width="2.5"
                      opacity=".6"
                    />{/each}
                  {#if showEdges}{#each edges as edge, i}{@const a =
                        rendered.get(
                          edge.prerequisite_milestone_id,
                        )!}{@const b = rendered.get(
                        edge.dependent_milestone_id,
                      )!}{@const highlighted =
                        active &&
                        connected.has(edge.prerequisite_milestone_id) &&
                        connected.has(edge.dependent_milestone_id)}<path
                        data-testid="dependency-edge"
                        data-dependency-id={edge.id}
                        data-highlighted={!!highlighted}
                        d={edgePath(
                          a.x,
                          a.y,
                          b.x,
                          b.y,
                          a.y === b.y ? (i % 3) * 8 : 0,
                        )}
                        fill="none"
                        stroke={highlighted
                          ? projectMap.get(
                              byID.get(edge.prerequisite_milestone_id)!
                                .project_id,
                            )?.color || "#7991bf"
                          : "#8a98b6"}
                        stroke-width={highlighted ? 2.5 : 1.5}
                        opacity={highlighted ? ".95" : ".26"}
                        stroke-dasharray={edge.dependency_type === "related_to"
                          ? "5 5"
                          : undefined}
                        marker-end="url(#arrow)"
                        class="dependency"
                      />{/each}{/if}
                  {#if scale(new Date()) > 0 && scale(new Date()) < width}<line
                      x1={scale(new Date())}
                      y1="0"
                      x2={scale(new Date())}
                      y2={height - 39}
                      class="today-line"
                    /><rect
                      x={scale(new Date()) - 24}
                      y={height - 58}
                      width="48"
                      height="20"
                      rx="5"
                      class="today-label"
                    /><text
                      x={scale(new Date())}
                      y={height - 44}
                      text-anchor="middle"
                      class="today-text">{t.today}</text
                    >{/if}
                  {#each milestones as m}{@const pos = rendered.get(
                      m.id,
                    )!}{@const color =
                      projectMap.get(m.project_id)?.color || "#7f9eff"}<g
                      role="button"
                      tabindex="0"
                      aria-label={m.title}
                      data-testid="milestone"
                      data-id={m.id}
                      class:selected={selected === m.id}
                      class="milestone"
                      style:--point={color}
                      opacity={active && !connected.has(m.id) ? ".42" : "1"}
                      onmouseenter={(e) => {
                        hovered = m.id;
                        tooltip = { m, x: e.clientX, y: e.clientY };
                      }}
                      onmouseleave={() => {
                        hovered = "";
                        tooltip = null;
                      }}
                      onfocus={() => (hovered = m.id)}
                      onblur={() => (hovered = "")}
                      onclick={() => choose(m)}
                      onkeydown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          choose(m);
                        }
                      }}
                      ><circle
                        cx={pos.x}
                        cy={pos.y}
                        r={selected === m.id || hovered === m.id ? 15 : 11}
                        fill={color}
                        opacity=".13"
                      /><circle
                        cx={pos.x}
                        cy={pos.y}
                        r="7"
                        fill={m.status === "completed"
                          ? color
                          : "var(--surface)"}
                        stroke={m.status === "delayed" ? "#f4b75e" : color}
                        stroke-width="2.5"
                        stroke-dasharray={m.status === "cancelled"
                          ? "3 2"
                          : undefined}
                      />{#if m.status === "completed"}<path
                          d={`M ${pos.x - 3} ${pos.y} l 2 2 l 4 -4`}
                          fill="none"
                          stroke="var(--background)"
                          stroke-width="1.5"
                        />{:else if m.status === "in_progress"}<circle
                          cx={pos.x}
                          cy={pos.y}
                          r="3"
                          fill={color}
                        />{/if}<text
                        x={pos.x + 15}
                        y={pos.y + 26}
                        class="milestone-title"
                        >{m.title.length > 23
                          ? m.title.slice(0, 22) + "…"
                          : m.title}</text
                      ><text
                        x={pos.x + 15}
                        y={pos.y + 44}
                        class="milestone-date">{date(m.planned_date)}</text
                      ><rect
                        x={pos.x - 7}
                        y={pos.y - 35}
                        width={Math.min(
                          142,
                          Math.max(48, m.version.length * 7 + 20),
                        )}
                        height="21"
                        rx="5"
                        fill={color}
                        opacity=".12"
                      /><text
                        x={pos.x + 3}
                        y={pos.y - 20}
                        class="milestone-type"
                        >{m.version || tr(m.milestone_type)}</text
                      >{#if showConflicts && conflictIDs.has(m.id)}<text
                          x={pos.x + 13}
                          y={pos.y + 4}
                          class="warning-icon">⚠</text
                        >{/if}<title
                        >{m.title} · {date(m.planned_date)} · {tr(
                          m.status,
                        )}</title
                      ></g
                    >{/each}
                  <line
                    x1="0"
                    y1={height - 45}
                    x2={width}
                    y2={height - 45}
                    class="axis"
                  /></svg
                >
              </div>
            </div>
            <div class="minimap-wrap">
              <button class="minimap" aria-label={t.timeline} onclick={mini}
                >{#each visibleProjects as p, i}<span
                    class="mini-lane"
                    style:top={`${18 + (i / Math.max(1, visibleProjects.length - 1)) * 60}%`}
                    style:background={p.color}
                  ></span>{/each}{#each milestones as m}{@const pos =
                    rendered.get(m.id)!}<i
                    style:left={`${(pos.x / width) * 100}%`}
                    style:top={`${18 + (visibleProjects.findIndex((p) => p.id === m.project_id) / Math.max(1, visibleProjects.length - 1)) * 60}%`}
                    style:background={projectMap.get(m.project_id)?.color}
                  ></i>{/each}<span
                  class="mini-window"
                  style:left={`${(scroll / width) * 100}%`}
                  style:width={`${Math.min(100, (viewWidth / width) * 100)}%`}
                ></span></button
              >
            </div>
            <div class="graph-footer">
              <span
                ><span class="key solid"></span>{t.requires}<span
                  class="key dashed"
                ></span>{t.related_to}</span
              ><span>{t.help}</span>
            </div>
          </section>{/if}
      {/if}
    </main>
    {#if current && page === "overview"}<aside
        class="details"
        aria-label={t.details}
      >
        <div class="details-heading">
          <div
            class="project-symbol"
            style:--project={projectMap.get(current.project_id)?.color}
          >
            ◇
          </div>
          <div>
            <h2>{current.title}</h2>
            <p>{projectMap.get(current.project_id)?.name}</p>
          </div>
          <button
            aria-label={t.close}
            onclick={() => {
              selected = "";
              simulation = false;
            }}>×</button
          >
        </div>
        <div class="detail-tabs">
          <span class="active">{t.details}</span><span
            >{t.dependencies}
            <b
              >{graph.dependencies.filter(
                (e) =>
                  e.dependent_milestone_id === current.id ||
                  e.prerequisite_milestone_id === current.id,
              ).length}</b
            ></span
          >
        </div>
        <div class="detail-content">
          <div class="metadata">
            <div>
              <span>{t.planned_date}</span><strong
                >{date(current.planned_date)}</strong
              >
            </div>
            <div>
              <span>{t.status}</span><strong class={`status ${current.status}`}
                ><i></i>{tr(current.status)}</strong
              >
            </div>
            <div>
              <span>{t.type}</span><strong>{tr(current.milestone_type)}</strong>
            </div>
            {#if current.completed_date}<div>
                <span>{t.completed_date}</span><strong
                  >{date(current.completed_date)}</strong
                >
              </div>{/if}
            <div>
              <span>{t.version}</span><strong>{current.version || "—"}</strong>
            </div>
            <div>
              <span>{t.owner}</span><strong>{current.owner || "—"}</strong>
            </div>
          </div>
          {#if current.description}<h3>{t.description}</h3>
            <p class="description">
              {current.description}
            </p>{/if}{#if safeURL(current.external_url)}<a
              href={safeURL(current.external_url)}
              target="_blank"
              rel="noopener noreferrer">{t.external_url} ↗</a
            >{/if}
          {#each conflicts.filter((c) => c.dependent_milestone_id === current.id) as c}<div
              class="conflict-card"
            >
              <strong>△ {t.warning}</strong>
              <p>
                {byID.get(c.prerequisite_milestone_id)?.title}: {t.delay}
                {c.days}
                {t.days}.
              </p>
            </div>{/each}
          <div class="section-title">
            <h3>{t.dependencies}</h3>
            <button
              aria-label={t.addDependency}
              onclick={() => open("dependency")}>＋</button
            >
          </div>
          {#each ["prerequisites", "dependents"] as direction}<div
              class="dependency-group"
            >
              <h4>{tr(direction)}</h4>
              {#each graph.dependencies.filter( (e) => (direction === "prerequisites" ? e.dependent_milestone_id === current.id : e.prerequisite_milestone_id === current.id) ) as e}{@const other =
                  byID.get(
                    direction === "prerequisites"
                      ? e.prerequisite_milestone_id
                      : e.dependent_milestone_id,
                  )}{#if other}<div class="dependency-row">
                    <button onclick={() => choose(other)}
                      ><i
                        style:background={projectMap.get(other.project_id)
                          ?.color}
                      ></i><span
                        >{other.title}<small
                          >{date(other.planned_date)} · {tr(
                            e.dependency_type,
                          )}</small
                        ></span
                      ></button
                    ><button
                      class="small"
                      aria-label={`${t.remove} ${other.title}`}
                      onclick={() => remove("dependencies", e.id)}>×</button
                    >
                  </div>{/if}{:else}<p class="muted">{t.noDeps}</p>{/each}
            </div>{/each}
          <h3>{t.impact}</h3>
          <p class="muted">
            {t.direct}: {impact.direct.length} · {t.transitive}: {impact
              .transitive.length}
          </p>
          <div class="impact-list">
            {#each [...new Set( [...impact.direct, ...impact.transitive] )] as id}{@const m =
                byID.get(id)}{#if m}<button onclick={() => choose(m)}
                  >{m.title} ↗</button
                >{/if}{/each}
          </div>
          <div class="whatif">
            <h3>⌁ {t.whatif}</h3>
            <label
              >{t.proposed}<input
                type="date"
                bind:value={whatDate}
                onchange={() => (simulation = false)}
              /></label
            ><button onclick={() => (simulation = true)} disabled={!whatDate}
              >{t.simulate}</button
            >{#if simulation}<p>{t.original}: {date(current.planned_date)}</p>
              <p>
                {t.affected}: {new Set([...impact.direct, ...impact.transitive])
                  .size}
              </p>
              <p>{t.newConflicts}: <strong>{newConflicts.length}</strong></p>
              {#each simConflicts as c}<p class="simulation-conflict">
                  △ {byID.get(c.dependent_milestone_id)?.title} ← {byID.get(
                    c.prerequisite_milestone_id,
                  )?.title} · {c.days}
                  {t.days}
                </p>{/each}<small>{t.simulationNote}</small>
              <div class="form-actions">
                <button onclick={() => (simulation = false)}>{t.cancel}</button
                ><button class="primary" onclick={apply}>{t.apply}</button>
              </div>{/if}
          </div>
        </div>
        <div class="detail-actions">
          <button class="primary" onclick={() => open("milestone", current.id)}
            >✎ {t.edit}</button
          ><button
            class="danger"
            onclick={() => remove("milestones", current.id)}>{t.delete}</button
          >
        </div>
      </aside>{/if}
  </div>
  {#if tooltip}<div
      class="tooltip"
      role="tooltip"
      style:left={`${Math.min(tooltip.x + 16, window.innerWidth - 270)}px`}
      style:top={`${Math.min(tooltip.y + 18, window.innerHeight - 150)}px`}
    >
      <strong>{tooltip.m.title}</strong><span
        >{date(tooltip.m.planned_date)}</span
      ><small
        >{projectMap.get(tooltip.m.project_id)?.name} · {tr(
          tooltip.m.status,
        )}</small
      >{#if tooltip.m.description}<p>{tooltip.m.description}</p>{/if}
    </div>{/if}
  <dialog bind:this={dialog} onclose={() => (modal = "")}>
    <form
      onsubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <div class="dialog-title">
        <h2>
          {modal === "project"
            ? editID
              ? t.editProject
              : t.addProject
            : modal === "milestone"
              ? editID
                ? t.edit
                : t.addMilestone
              : t.addDependency}
        </h2>
        <button type="button" aria-label={t.close} onclick={close}>×</button>
      </div>
      {#if error}<div class="error" role="alert">{error}</div>{/if}
      {#if modal === "project"}<label
          >{t.name}<input
            required
            maxlength="120"
            bind:value={form.name}
          /></label
        >
        <div class="form-grid">
          <label>{t.slug}<input pattern="[^ ]+" bind:value={form.slug} /></label
          ><label>{t.color}<input type="color" bind:value={form.color} /></label
          >
        </div>
        <label>{t.icon}<input maxlength="8" bind:value={form.icon} /></label
        ><label
          >{t.description}<textarea bind:value={form.description}
          ></textarea></label
        >
      {:else if modal === "milestone"}<label
          >{t.title}<input
            required
            maxlength="200"
            bind:value={form.title}
          /></label
        ><label
          >{t.project}<select bind:value={form.project_id} required
            >{#each projects as p}<option value={p.id}>{p.name}</option
              >{/each}</select
          ></label
        >
        <div class="form-grid">
          <label
            >{t.planned_date}<input
              type="date"
              required
              bind:value={form.planned_date}
            /></label
          ><label
            >{t.completed_date}<input
              type="date"
              required={form.status === "completed"}
              bind:value={form.completed_date}
            /></label
          ><label
            >{t.status}<select bind:value={form.status}
              >{#each statuses as s}<option value={s}>{tr(s)}</option
                >{/each}</select
            ></label
          ><label
            >{t.type}<select bind:value={form.milestone_type}
              >{#each types as s}<option value={s}>{tr(s)}</option
                >{/each}</select
            ></label
          ><label>{t.version}<input bind:value={form.version} /></label><label
            >{t.owner}<input bind:value={form.owner} /></label
          >
        </div>
        <label
          >{t.external_url}<input
            type="url"
            pattern="https?://.*"
            bind:value={form.external_url}
          /></label
        ><label
          >{t.description}<textarea bind:value={form.description}
          ></textarea></label
        >
      {:else if modal === "dependency"}<label
          >{t.prerequisite}<select
            required
            bind:value={form.prerequisite_milestone_id}
            >{#each graph.milestones as m}<option value={m.id}
                >{projectMap.get(m.project_id)?.name} · {m.title}</option
              >{/each}</select
          ></label
        >
        <div class="direction-hint">↓ {t.requires} ↑</div>
        <label
          >{t.dependent}<select
            required
            bind:value={form.dependent_milestone_id}
            >{#each graph.milestones as m}<option value={m.id}
                >{projectMap.get(m.project_id)?.name} · {m.title}</option
              >{/each}</select
          ></label
        ><label
          >{t.connection}<select bind:value={form.dependency_type}
            ><option value="requires">{t.requires}</option><option
              value="related_to">{t.related_to}</option
            ></select
          ></label
        >{/if}
      <div class="form-actions">
        <button type="button" onclick={close}>{t.cancel}</button><button
          class="primary"
          type="submit">{t.save}</button
        >
      </div>
    </form>
  </dialog>
{/if}
