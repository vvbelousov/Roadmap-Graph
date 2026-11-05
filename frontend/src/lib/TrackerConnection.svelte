<script lang="ts">
  import { onMount } from "svelte";
  import { trackerEn, trackerRu, trackerMessage } from "../i18n/tracker";
  let {
    api,
    lang,
    onproject = (_id: string) => {},
    onDirtyChange = (_dirty: boolean) => {},
  }: {
    api: (path: string, method?: string, body?: any) => Promise<any>;
    lang: string;
    onproject?: (id: string) => void;
    onDirtyChange?: (dirty: boolean) => void;
  } = $props();
  let t = $derived(lang === "ru" ? trackerRu : trackerEn);
  let data = $state<any>(null),
    config = $state<any>(null),
    busy = $state(false),
    error = $state(""),
    message = $state(""),
    saved = $state("");
  let dirty = $derived(Boolean(saved) && JSON.stringify(config) !== saved);
  $effect(() => onDirtyChange(dirty));
  async function refresh(initial = false) {
    try {
      data = await api("/tracker");
      if (initial) {
        config = JSON.parse(JSON.stringify(data.config));
        saved = JSON.stringify(config);
      }
    } catch (e) {
      error = String(e);
    }
  }
  onMount(() => {
    refresh(true);
    const timer = setInterval(() => refresh(), 5000);
    const unload = (e: BeforeUnloadEvent) => {
      if (dirty) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", unload);
    return () => {
      clearInterval(timer);
      window.removeEventListener("beforeunload", unload);
    };
  });
  async function run(fn: () => Promise<void>) {
    busy = true;
    error = "";
    message = "";
    try {
      await fn();
      await refresh();
    } catch (e) {
      error = String(e);
    } finally {
      busy = false;
    }
  }
  async function save() {
    config = await api("/tracker/config", "PUT", config);
    saved = JSON.stringify(config);
    message = t.saved;
  }
</script>

<section aria-label={t.heading} class="tracker-connection">
  <h2>{t.heading}</h2>
  {#if error}<p class="error" role="alert">
      {trackerMessage(error, lang)}
    </p>{/if}
  {#if message}<p role="status">{message}</p>{/if}
  {#if config && data}
    <label
      ><input type="checkbox" bind:checked={config.enabled} />{t.enabled}</label
    >
    <label>{t.org}<input bind:value={config.org_id} /></label>
    <label
      >{t.orgType}<select bind:value={config.org_header}
        ><option value="X-Org-ID">Yandex 360</option><option
          value="X-Cloud-Org-ID">Yandex Identity Hub</option
        ></select
      ></label
    >
    <label
      >{t.auth}<select bind:value={config.auth}
        ><option>OAuth</option><option>Bearer</option></select
      ></label
    >
    <p>{t.secret}</p>
    <p>
      {t.tokenAvailability}: {data.token_available
        ? t.tokenAvailable
        : t.tokenMissing}
    </p>
    <label
      >{t.interval}<input
        type="number"
        min="30"
        max="86400"
        bind:value={config.poll_interval_seconds}
      /></label
    >
    <div class="actions">
      <button disabled={busy} onclick={() => run(save)}>{t.save}</button><button
        disabled={busy}
        onclick={() =>
          run(async () => {
            await api("/tracker/test", "POST", {});
            message = t.connected;
          })}>{t.test}</button
      >
      <button
        disabled={busy || !data.config.enabled || data.running}
        onclick={() =>
          run(async () => {
            await api("/tracker/sync", "POST", {});
            message = t.scheduled;
          })}>{t.syncAll}</button
      >
      <button
        disabled={busy}
        onclick={() =>
          run(async () => {
            await api("/tracker/retry", "POST", {});
            message = t.saved;
          })}>{t.retryConnection}</button
      >
    </div>
    <p>
      {t.lastSuccess}: {data.last_success || "—"}
      {data.running ? t.running : ""}
    </p>
    {#if data.last_error}<p role="alert">
        {trackerMessage(data.last_error, lang)}
      </p>{/if}
    {#if data.warning}<p role="alert">
        {trackerMessage(data.warning, lang)}
      </p>{/if}
    <h3>{t.projectSettings}</h3>
    <p>{t.projectSettingsHint}</p>
    {#each data.projects || [] as project}<article>
        <button onclick={() => onproject(project.project_id)}
          >{project.name} →</button
        ><span
          >{!project.configured
            ? t.notConfigured
            : project.enabled
              ? t.syncEnabled
              : t.syncDisabled}</span
        >
        <p>{t.lastSuccess}: {project.last_success || "—"}</p>
        {#if project.last_error}<p role="alert">
            {trackerMessage(project.last_error, lang)}
          </p>{/if}{#if project.warning}<p>
            {trackerMessage(project.warning, lang)}
          </p>{/if}
      </article>{/each}
  {/if}
</section>

<style>
  .tracker-connection {
    margin-top: 24px;
    overflow-wrap: anywhere;
  }
  .tracker-connection label {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .tracker-connection input:not([type="checkbox"]),
  select {
    min-width: 0;
    max-width: 100%;
    flex: 1;
  }
  .tracker-connection input[type="checkbox"] {
    width: auto;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin: 16px 0;
  }
  article {
    border-top: 1px solid var(--border);
    padding: 12px 0;
  }
  article span {
    display: block;
    margin-top: 8px;
  }
</style>
