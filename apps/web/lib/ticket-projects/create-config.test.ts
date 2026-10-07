import { describe, expect, it } from "vitest";
import {
  defaultCreateConfig,
  serializeCreateConfig,
} from "@/lib/ticket-projects/create-config";

describe("kaneo create config", () => {
  it("defaults include an empty workspaceId", () => {
    expect(defaultCreateConfig("kaneo")).toEqual({
      workspaceId: "",
      status: "ready",
      priority: "medium",
    });
  });

  it("persists the selected workspace on create", () => {
    expect(
      serializeCreateConfig(
        "kaneo",
        { status: "in-progress", priority: "high" },
        " ws-1 ",
      ),
    ).toEqual({ workspaceId: "ws-1", status: "in-progress", priority: "high" });
  });

  it("keeps the stored workspace when editing", () => {
    expect(
      serializeCreateConfig("kaneo", {
        workspaceId: "ws-2",
        status: "ready",
        priority: "medium",
      }),
    ).toEqual({ workspaceId: "ws-2", status: "ready", priority: "medium" });
  });
});

describe("linear create config", () => {
  it("coerces priority to a number", () => {
    expect(
      serializeCreateConfig("linear", { priority: "3", stateId: "state-1" }),
    ).toEqual({ priority: 3, stateId: "state-1" });
  });
});
