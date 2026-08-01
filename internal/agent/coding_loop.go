package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const CodingAgentSystemPrompt = `You are a careful coding agent. Work only toward the requested goal.

Use exactly one JSON object per turn, with this schema:
{"action":"tool_name","input":{...}}

Inspect before changing files. Make focused changes using apply_patch, then run a relevant run_check. Do not claim success without checking when a check exists. Never request network access, shell commands, credentials, or paths outside the workspace. If a tool fails, inspect the error and choose a safe correction. When complete, use finish.

Available tools:
%s`

// RunCodingAgentLoop is a direct tool-call loop: one model decision per step.
// It is intentionally separate from RunAgentLoop so existing integrations can
// opt in without changing legacy planner/executor/critic behavior.
func RunCodingAgentLoop(goalID, goal, workspace string, maxSteps int) (string, error) {
	state, err := LoadOrInitializeState(goalID, goal)
	if err != nil {
		return "", err
	}
	registry, err := NewCodingToolRegistry(workspace)
	if err != nil {
		return "", err
	}
	if state.Done {
		return state.FinalOutput, nil
	}

	for step := 1; step <= maxSteps; step++ {
		Broadcast(goalID, "step_start", map[string]interface{}{"step": step, "mode": "coding"})
		Broadcast(goalID, "deciding", nil)
		decision, err := codingDecision(state, registry)
		if err != nil {
			state.Errors = append(state.Errors, fmt.Sprintf("agent decision: %v", err))
			_ = PersistState(state)
			Broadcast(goalID, "error", map[string]interface{}{"error": err.Error()})
			continue
		}
		if decision.Action == "finish" {
			state.Done = true
			state.FinalOutput, _ = decision.Input["final_output"].(string)
			if strings.TrimSpace(state.FinalOutput) == "" {
				state.FinalOutput = "Goal achieved."
			}
			_ = PersistState(state)
			Broadcast(goalID, "finished", map[string]interface{}{"final_output": state.FinalOutput})
			return state.FinalOutput, nil
		}

		Broadcast(goalID, "action_start", decision)
		result, execErr := registry.Execute(decision.Action, decision.Input)
		if execErr != nil {
			result = "Error: " + execErr.Error() + "\n" + result
		}
		state.CurrentTask = decision.Action
		state.PastActions = append(state.PastActions, ActionRecord{Action: decision.Action, Input: decision.Input})
		state.Results = append(state.Results, result)
		if execErr != nil {
			state.Errors = append(state.Errors, result)
		}
		_ = PersistState(state)
		Broadcast(goalID, "action_result", map[string]interface{}{"action": decision.Action, "result": result})
	}
	return "", fmt.Errorf("coding agent reached max steps (%d) without finishing", maxSteps)
}

func codingDecision(state *AgentState, registry *CodingToolRegistry) (*ExecutorDecision, error) {
	systemPrompt := fmt.Sprintf(CodingAgentSystemPrompt, registry.Descriptions())
	userPrompt := fmt.Sprintf("Goal: %s\nWorkspace: %s\n\nRecent state:\n%v\n\nChoose the next action.", state.Goal, registry.Workspace(), state.Compact())
	response, err := CallLLM(systemPrompt, userPrompt, true)
	if err != nil {
		return nil, err
	}
	var decision ExecutorDecision
	if err := json.Unmarshal([]byte(response), &decision); err != nil {
		return nil, fmt.Errorf("invalid tool decision: %w", err)
	}
	if decision.Input == nil {
		decision.Input = map[string]interface{}{}
	}
	return &decision, nil
}

func CodingAgentWorkspace() string { return os.Getenv("CODING_AGENT_WORKSPACE") }
