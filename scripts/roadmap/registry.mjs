/** @typedef {{ href: string, label: string, note?: string }} DocRef */
/** @typedef {import('./spec-links.mjs').describe} DescribeFn */

/**
 * @param {DescribeFn} describe
 * @returns {{ tasks: object[], epics: object[], leaves: object[] }}
 */
export function createRegistry(describe) {
  /** @type {object[]} */
  const tasks = [];

  function epic(id, title, domain, opts = {}) {
    const description =
      opts.description ??
      describe({
        context: opts.context,
        specs: opts.specs ?? [],
        stack: opts.stack ?? [],
        schema: opts.schema ?? [],
        implementation: opts.implementation ?? [],
        acceptance: opts.acceptance ?? [],
      });
    const task = {
      id,
      type: "epic",
      title,
      domain,
      priority: opts.priority ?? "high",
      depends_on: opts.depends_on ?? [],
      doc_ref: [
        ...new Set([
          ...(opts.specs ?? []).map((s) => s.href),
          ...(opts.stack ?? []).map((s) => s.href),
          ...(opts.schema ?? []).map((s) => s.href),
        ]),
      ],
      description: withRoadmapId(id, description),
      lane: "S",
    };
    tasks.push(task);
    return task;
  }

  function leaf(id, parent, title, domain, opts = {}) {
    const description = describe({
      context: opts.context,
      specs: opts.specs ?? [],
      stack: opts.stack ?? [],
      schema: opts.schema ?? [],
      implementation: opts.implementation ?? [],
      acceptance: opts.acceptance ?? [],
      files: opts.files ?? [],
      tests: opts.tests ?? [],
      outOfScope: opts.outOfScope ?? [],
      relatedTasks: opts.relatedTasks ?? [],
    });
    const acceptance_criteria = opts.acceptance ?? [];
    const task = {
      id,
      type: "leaf",
      parent,
      title,
      domain,
      priority: opts.priority ?? "medium",
      depends_on: opts.depends_on ?? [],
      doc_ref: [
        ...new Set([
          ...(opts.specs ?? []).map((s) => s.href),
          ...(opts.stack ?? []).map((s) => s.href),
          ...(opts.schema ?? []).map((s) => s.href),
        ]),
      ],
      description: withRoadmapId(id, description),
      acceptance_criteria,
      files: opts.files ?? [],
      tests: opts.tests ?? [],
      lane: opts.lane ?? "S",
    };
    tasks.push(task);
    return task;
  }

  return { tasks, epic, leaf };
}

/** Phasical intake idempotency key — searchable in task descriptions. */
function withRoadmapId(id, description) {
  const marker = `**Roadmap ID:** ${id}`;
  if (description.includes(marker)) return description;
  return `${description}\n\n---\n\n${marker}`;
}
