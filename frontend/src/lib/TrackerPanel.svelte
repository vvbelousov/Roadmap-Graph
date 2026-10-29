<script lang="ts">
  import { onMount } from "svelte";
  import { trackerEn, trackerRu, trackerMessage } from "../i18n/tracker";
  import type { Project } from "./graph";
  let {
    api,
    lang,
    projects = [],
    milestoneID = "",
    onchange = () => {},
  }: {
    api: (path: string, method?: string, body?: any) => Promise<any>;
    lang: string;
    projects?: Project[];
    milestoneID?: string;
    onchange?: () => void | Promise<void>;
  } = $props();
  let t = $derived(lang === "ru" ? trackerRu : trackerEn);
  let data = $state<any>(null),
    config = $state<any>(null),
    preview = $state<any>(null),
    previewStatusChoices = $state<Record<string, string>>({}),
    busy = $state(false),
    message = $state(""),
    error = $state(""),
    advanced = $state(false);
  let fieldNames = [
    "title",
    "description",
    "planned_date",
    "completed_date",
    "status",
    "owner",
    "version",
  ];
  let statusRows = $state<{ remote: string; local: string; write: boolean }[]>(
    [],
  );
  let choices = $state<Record<string, string>>({}),
    destination = $state(0);
  const statuses = [
    "planned",
    "in_progress",
    "completed",
    "delayed",
    "cancelled",
  ];
  let record = $derived(
    data?.records?.find((r: any) => r.local_id === milestoneID),
  );
  let pending = $derived(
    data?.outbox?.filter((o: any) => o.local_id === milestoneID) || [],
  );
  let conflictSignature = $derived(JSON.stringify(record?.conflicts || {}));
  $effect(() => {
    milestoneID;
    conflictSignature;
    choices = {};
  });
  async function refresh(initial = false) {
    try {
      const d = await api("/tracker");
      const changed =
        data && JSON.stringify(data.records) !== JSON.stringify(d.records);
      data = d;
      if (changed && !initial) await onchange();
      if (initial) {
        applyConfig(d.config);
      }
    } catch (e) {
      error = String(e);
    }
  }
  onMount(() => {
    refresh(true);
    const id = setInterval(() => refresh(), 5000);
    return () => clearInterval(id);
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
  async function previewImport() {
    preview = await api("/tracker/preview", "POST", {});
    previewStatusChoices = Object.fromEntries(
      (preview.unmapped_statuses || []).map((status: any) => [status.key, ""]),
    );
  }
  async function savePreviewStatuses() {
    for (const status of preview?.unmapped_statuses || []) {
      const local = previewStatusChoices[status.key];
      if (!statuses.includes(local)) throw new Error(t.chooseStatus);
      const row = statusRows.find((row) => row.remote === status.key);
      if (row) row.local = local;
      else statusRows.push({ remote: status.key, local, write: false });
    }
    await save();
    await previewImport();
  }
  function applyConfig(next: any) {
    if (!next) return;
    config = JSON.parse(JSON.stringify(next));
    statusRows = Object.entries(config.statuses || {}).map(
      ([remote, local]) => ({
        remote,
        local: String(local),
        write: config.write_statuses?.[String(local)] === remote,
      }),
    );
  }
  function setWriteStatus(index: number, enabled: boolean) {
    const row = statusRows[index];
    row.write = enabled;
    if (enabled) {
      for (let i = 0; i < statusRows.length; i++) {
        if (i !== index && statusRows[i].local === row.local)
          statusRows[i].write = false;
      }
    }
  }
  function setLocalStatus(index: number, value: string) {
    statusRows[index].local = value;
    if (statusRows[index].write) setWriteStatus(index, true);
  }
  async function save() {
    const next = JSON.parse(JSON.stringify(config));
    next.statuses = {};
    next.write_statuses = {};
    for (const row of statusRows) {
      if (!row.remote) continue;
      if (Object.hasOwn(next.statuses, row.remote))
        throw new Error(t.duplicateStatus);
      next.statuses[row.remote] = row.local;
      if (row.write) {
        if (Object.hasOwn(next.write_statuses, row.local))
          throw new Error(t.oneOutgoingStatus);
        next.write_statuses[row.local] = row.remote;
      }
    }
    const saved = await api("/tracker/config", "PUT", next);
    applyConfig(saved);
    preview = null;
    message = t.saved;
  }
  async function action(path: string, body: any = {}) {
    await api(path, "POST", body);
    await onchange();
  }
  async function resolve() {
    await action(`/tracker/milestones/${milestoneID}/resolve`, { choices });
    choices = {};
  }
  function addField(k: string) {
    config.fields[k] = {
      field: (
        {
          title: "summary",
          description: "description",
          planned_date: "dueDate",
          completed_date: "",
          status: "status",
          owner: "assignee",
          version: "fixVersions",
        } as Record<string, string>
      )[k],
      codec: k.includes("date")
        ? "date"
        : k === "status"
          ? "status"
          : k === "owner"
            ? "id"
            : k === "version"
              ? "single_id"
              : "string",
      writable: k !== "status",
    };
  }
  function removeField(k: string) {
    delete config.fields[k];
  }
</script>

{#if !milestoneID || data?.config?.enabled || record || pending.length}
  <section class="tracker-panel" aria-label={t.heading}>
    <h2>{t.heading}</h2>
    {#if error && (milestoneID || !config)}<p class="error" role="alert">
        {trackerMessage(error, lang)}
      </p>{/if}
    {#if message && milestoneID}<p role="status">{message}</p>{/if}
    {#if data && config}
      {#if milestoneID}
        <p>
          <strong
            >{t.state}: {record?.state === "conflict"
              ? t.states.conflict
              : record && record.state !== "synchronized"
                ? t.states[record.state as keyof typeof t.states] ||
                  record.state
                : pending.length
                  ? t.states.pending_upload
                  : record
                    ? t.states.synchronized
                    : t.states.unlinked}</strong
          >
        </p>
        {#if record}
          <a href={record.url} target="_blank" rel="noopener noreferrer"
            >{record.issue_key} ↗</a
          >
          {#if record.error}<p role="alert">
              {trackerMessage(record.error, lang)}
            </p>{/if}
          {#if record.state === "conflict"}
            <h3>{t.syncConflict}</h3>
            {#each Object.entries(record.conflicts) as [field, raw]}
              {@const c = raw as {
                local: string;
                remote: string;
                baseline: string;
              }}
              <fieldset>
                <legend
                  >{t.fields[field as keyof typeof t.fields] || field}</legend
                >
                <p>{t.baseline}: {c.baseline || "∅"}</p>
                <label
                  ><input
                    type="radio"
                    name={`resolve-${field}`}
                    value="local"
                    bind:group={choices[field]}
                  />{t.keepLocal}: {c.local || "∅"}</label
                >
                <label
                  ><input
                    type="radio"
                    name={`resolve-${field}`}
                    value="remote"
                    bind:group={choices[field]}
                  />{t.keepRemote}: {c.remote || "∅"}</label
                >
              </fieldset>
            {/each}
            <button
              disabled={busy ||
                Object.keys(choices).length !==
                  Object.keys(record.conflicts).length}
              onclick={() => run(resolve)}>{t.resolve}</button
            >
          {/if}
          {#if record.state !== "unlinked"}<button
              disabled={busy}
              onclick={() =>
                run(() => action(`/tracker/milestones/${milestoneID}/unlink`))}
              >{t.unlink}</button
            >{/if}
          <details>
            <summary>{t.mappings}</summary>
            <p class="hint">{t.mappingHint}</p>
            <ul>
              {#each fieldNames as k}<li>
                  {t.fields[k as keyof typeof t.fields]}: {config.fields[k]
                    ? config.fields[k].writable
                      ? t.bidirectional
                      : t.readOnly
                    : t.localOnly}
                </li>{/each}
            </ul>
          </details>
        {:else if config.enabled}
          <label
            >{t.destination}<select bind:value={destination}
              >{#each config.selections as s, i}{#if s.queue}<option value={i}
                    >{s.queue}</option
                  >{/if}{/each}</select
            ></label
          >
          <button
            disabled={busy ||
              !config.selections[destination]?.queue ||
              pending.some((o: any) => o.kind === "publish")}
            onclick={() =>
              run(() =>
                action(`/tracker/milestones/${milestoneID}/publish`, {
                  selection: destination,
                }),
              )}>{t.publish}</button
          >
        {/if}
        {#each pending as op}{#if op.error}<p role="alert">
              {trackerMessage(op.error, lang)} ({op.attempts}/8)
            </p>{/if}{/each}
      {:else}
        <p>{t.intro}</p>
        <label
          ><input
            type="checkbox"
            bind:checked={config.enabled}
          />{t.enabled}</label
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
        <p class="hint">{t.secret}</p>
        <label
          >{t.interval}<input
            type="number"
            min="30"
            max="86400"
            bind:value={config.poll_interval_seconds}
          /></label
        >
        <h3>{t.selections}</h3>
        <p class="hint">{t.queueHint}</p>
        {#each config.selections as s, i}
          <fieldset>
            <legend>{t.selection} {i + 1}</legend>
            <label
              >{t.queue}<input
                bind:value={s.queue}
                disabled={!!s.query}
              /></label
            >
            <label
              >{t.query}<input
                bind:value={s.query}
                disabled={!!s.queue}
              /></label
            >
            <label
              >{t.project}<select bind:value={s.project_id}
                ><option value="">{t.choose}</option
                >{#each projects as p}<option value={p.id}>{p.name}</option
                  >{/each}</select
              ></label
            >
            <button onclick={() => config.selections.splice(i, 1)}
              >{t.remove}</button
            >
          </fieldset>
        {/each}
        <button
          onclick={() =>
            config.selections.push({
              queue: "",
              query: "",
              project_id: projects[0]?.id || "",
            })}>{t.addSelection}</button
        >
        <h3>{t.statuses}</h3>
        <p class="hint">{t.statusHint}</p>
        {#if statusRows.some((row) => row.write) && !config.fields.status?.writable}<p
            role="status"
          >
            {t.statusWriteDisabled}
          </p>{/if}
        {#each statusRows as row, i}<div class="mapping-row">
            <label>{t.remoteStatus}<input bind:value={row.remote} /></label>
            <label
              >{t.localStatus}<select
                value={row.local}
                aria-label={`${t.localStatus}: ${row.remote || i + 1}`}
                onchange={(event) =>
                  setLocalStatus(i, event.currentTarget.value)}
                >{#each statuses as s}<option value={s}
                    >{t.states[s as keyof typeof t.states] || s}</option
                  >{/each}</select
              ></label
            >
            <label
              ><input
                type="checkbox"
                checked={row.write}
                aria-label={`${t.writeStatus}: ${row.remote || i + 1}`}
                onchange={(event) =>
                  setWriteStatus(i, event.currentTarget.checked)}
              />{t.writeStatus}</label
            >
            <button onclick={() => statusRows.splice(i, 1)}>{t.remove}</button>
          </div>{/each}
        <button
          onclick={() =>
            statusRows.push({ remote: "", local: "planned", write: false })}
          >{t.addStatus}</button
        >
        <button aria-expanded={advanced} onclick={() => (advanced = !advanced)}
          >{t.mappings}</button
        >
        {#if advanced}
          <p class="hint">{t.fieldHint}</p>
          {#each fieldNames as k}<fieldset>
              <legend>{t.fields[k as keyof typeof t.fields]}</legend>
              {#if config.fields[k]}
                <label
                  >{t.trackerField}<input
                    bind:value={config.fields[k].field}
                  /></label
                >
                <label
                  >{t.format}<select bind:value={config.fields[k].codec}
                    >{#each ["string", "date", "id", "single_id", "status"] as codec}<option
                        value={codec}>{codec}</option
                      >{/each}</select
                  ></label
                >
                <label
                  ><input
                    type="checkbox"
                    bind:checked={config.fields[k].writable}
                  />{t.allowWrite}</label
                >
                {#if !["title", "planned_date"].includes(k)}<button
                    onclick={() => removeField(k)}>{t.remove}</button
                  >{/if}
              {:else}<p>{t.localOnly}</p>
                <button onclick={() => addField(k)}>{t.mapField}</button>{/if}
            </fieldset>{/each}
          <h3>{t.links}</h3>
          <p>{t.directionHint}</p>
          {#each config.links as link, i}<fieldset>
              <legend>{t.link} {i + 1}</legend>
              <label>{t.linkType}<input bind:value={link.type} /></label>
              <label
                >{t.direction}<select bind:value={link.direction}
                  ><option>outward</option><option>inward</option></select
                ></label
              >
              <label
                >{t.relationship}<select bind:value={link.local_type}
                  ><option value="requires">{t.requires}</option><option
                    value="related_to">{t.related}</option
                  ></select
                ></label
              >
              <label
                >{t.writeRelationship}<select bind:value={link.relationship}
                  >{#each ["is dependent by", "depends on", "relates", "is subtask for", "is parent task for"] as r}<option
                      >{r}</option
                    >{/each}</select
                ></label
              >
              <button onclick={() => config.links.splice(i, 1)}
                >{t.remove}</button
              >
            </fieldset>{/each}
          <button
            onclick={() =>
              config.links.push({
                type: "",
                direction: "outward",
                local_type: "requires",
                relationship: "is dependent by",
              })}>{t.addLink}</button
          >
        {/if}
        {#if error}<p
            class="error"
            role="alert"
            data-testid="tracker-save-error"
          >
            {trackerMessage(error, lang)}
          </p>{/if}
        {#if message}<p role="status">{message}</p>{/if}
        <div class="tracker-actions">
          <button disabled={busy} onclick={() => run(save)}>{t.save}</button>
          <button
            disabled={busy}
            onclick={() =>
              run(async () => {
                await action("/tracker/test");
                message = t.connected;
              })}>{t.test}</button
          >
          <button
            disabled={busy || !data.config.enabled}
            onclick={() => run(previewImport)}>{t.preview}</button
          >
        </div>
        {#if preview}<section aria-label={t.preview}>
            <h3>{t.preview}</h3>
            <p>{t.previewHint}</p>
            <p>
              {t.projects}: {preview.projects.length} · {t.milestones}: {preview
                .milestones.length} · {t.links}: {preview.links.length}
            </p>
            {#if preview.milestones.length === 0 && preview.skipped.length > 0}<p
                role="status"
              >
                {t.importBlocked}
              </p>{/if}
            {#if preview.unmapped_statuses?.length}
              <section aria-label={t.unmappedStatuses}>
                <h4>{t.unmappedStatuses}</h4>
                <p>{t.unmappedStatusHint}</p>
                {#each preview.unmapped_statuses as status}
                  <label
                    >{status.name} ({status.key}) · {status.count}
                    <select
                      aria-label={`${t.localStatus}: ${status.name} (${status.key})`}
                      value={previewStatusChoices[status.key] || ""}
                      onchange={(event) => {
                        previewStatusChoices = {
                          ...previewStatusChoices,
                          [status.key]: event.currentTarget.value,
                        };
                      }}
                    >
                      <option value="">{t.chooseStatus}</option>
                      {#each statuses as local}<option value={local}
                          >{t.states[local as keyof typeof t.states]}</option
                        >{/each}
                    </select>
                  </label>
                {/each}
                {#if Object.values(previewStatusChoices).includes("completed") && !config.fields.completed_date}<p
                  >
                    {t.completionDateHint}
                  </p>{/if}
                <button
                  disabled={busy ||
                    preview.unmapped_statuses.some(
                      (status: any) =>
                        !statuses.includes(previewStatusChoices[status.key]),
                    )}
                  onclick={() => run(savePreviewStatuses)}
                  >{t.saveStatusesAndPreview}</button
                >
              </section>
            {/if}
            {#each preview.projects as p}<p>{p.name}</p>{/each}
            {#if preview.milestones.length}<ul>
                {#each preview.milestones as item}<li>
                    {item.issue_key}: {item.milestone.title} — {item.milestone
                      .planned_date}
                  </li>{/each}
              </ul>{/if}
            {#if preview.links.length}<ul>
                {#each preview.links as link}<li>
                    {preview.milestones.find(
                      (m: any) =>
                        m.milestone.id === link.prerequisite_milestone_id,
                    )?.issue_key || link.prerequisite_milestone_id} → {preview.milestones.find(
                      (m: any) =>
                        m.milestone.id === link.dependent_milestone_id,
                    )?.issue_key || link.dependent_milestone_id} ({link.dependency_type ===
                    "requires"
                      ? t.requires
                      : t.related})
                  </li>{/each}
              </ul>{/if}
            <h4>{t.skipped}</h4>
            <ul>
              {#each preview.skipped as notice}<li>
                  <strong>{notice.issue_key}</strong>
                  {#each notice.reasons?.length ? notice.reasons : [notice.reason] as reason}<div
                    >
                      {trackerMessage(reason, lang)}
                    </div>{/each}
                </li>{/each}
            </ul>
            <h4>{t.warnings}</h4>
            <ul>
              {#each preview.warnings as notice}<li>
                  {notice.issue_key}: {trackerMessage(notice.reason, lang)}
                </li>{/each}
            </ul>
            <button
              disabled={busy ||
                preview.milestones.length +
                  preview.projects.length +
                  preview.links.length ===
                  0}
              onclick={() =>
                run(async () => {
                  await action("/tracker/import", { preview_id: preview.id });
                  preview = null;
                  message = t.imported;
                })}>{t.confirm}</button
            >
          </section>{/if}
        <p>
          {t.lastSuccess}: {data.last_success || "—"}
          {data.running ? t.running : ""}
        </p>
        {#if data.last_error}<p role="alert">
            {trackerMessage(data.last_error, lang)}
          </p>{/if}
        <ul>
          {#each data.records as r}<li>
              <a href={r.url} target="_blank" rel="noopener noreferrer"
                >{r.issue_key}</a
              >: {t.states[r.state as keyof typeof t.states] ||
                r.state}{#if r.error}
                — {trackerMessage(r.error, lang)}{/if}
            </li>{/each}
        </ul>
        {#each data.outbox as op}{#if op.error}<p role="alert">
              {op.kind}: {trackerMessage(op.error, lang)} ({op.attempts}/8)
            </p>{/if}{/each}
      {/if}
      <div class="tracker-actions">
        <button
          disabled={busy || !data.config.enabled || data.running}
          onclick={() =>
            run(async () => {
              await action("/tracker/sync");
              message = t.scheduled;
            })}>{t.sync}</button
        >
        <button
          disabled={busy}
          onclick={() => run(() => action("/tracker/retry"))}>{t.retry}</button
        >
      </div>
    {/if}
  </section>
{/if}

<style>
  .tracker-panel {
    border-top: 1px solid var(--border, #667085);
    padding-top: 1rem;
    margin-top: 1rem;
    max-width: 760px;
    overflow-wrap: anywhere;
  }
  .tracker-panel fieldset {
    border: 1px solid var(--border, #667085);
    border-radius: 8px;
    margin: 12px 0;
    padding: 12px;
    min-width: 0;
  }
  .tracker-panel label {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin: 8px 0;
  }
  .tracker-panel input:not([type="checkbox"]):not([type="radio"]),
  .tracker-panel select {
    max-width: 100%;
    min-width: 0;
    flex: 1;
  }
  .tracker-panel input[type="checkbox"],
  .tracker-panel input[type="radio"] {
    width: auto;
  }
  .tracker-actions,
  .mapping-row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    margin: 12px 0;
  }
  .hint {
    font-size: 0.85rem;
    opacity: 0.8;
  }
  .tracker-panel button {
    margin: 4px 0;
  }
  .tracker-panel ul {
    padding-left: 20px;
  }
</style>
