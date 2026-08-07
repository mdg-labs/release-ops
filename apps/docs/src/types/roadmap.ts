export interface RoadmapTaskBase {
  id: string;
  type: "epic" | "leaf";
  title: string;
  domain: string;
  description: string;
  depends_on?: string[];
  lane?: string;
}

export interface RoadmapEpic extends RoadmapTaskBase {
  type: "epic";
  doc_ref?: string[];
}

export interface RoadmapLeaf extends RoadmapTaskBase {
  type: "leaf";
  parent: string;
  acceptance_criteria?: string[];
  files?: string[];
}

export type RoadmapTask = RoadmapEpic | RoadmapLeaf;

export interface RoadmapDocument {
  version: number;
  format: string;
  generated: string;
  spec_ref: string;
  project: string;
  integration_branch: string;
  tasks: RoadmapTask[];
  meta: {
    epic_count: number;
    leaf_count: number;
    spec_audit?: {
      passed: boolean;
      exit_code: number;
      stdout: string;
    };
  };
}

export function isRoadmapLeaf(task: RoadmapTask): task is RoadmapLeaf {
  return task.type === "leaf" && typeof task.parent === "string";
}

export function isRoadmapEpic(task: RoadmapTask): task is RoadmapEpic {
  return task.type === "epic";
}
