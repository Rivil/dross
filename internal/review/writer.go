package review

// WriterAgent is the subagent_type of the dross-installed writer definition
// (assets/agents/dross-task-writer.md): the one subagent a solo execute run
// may hand a task's code to.
const WriterAgent = "dross-task-writer"

// WriterReportFence is the info string of the one fenced block the writer
// ends its reply with — the only part of its work the main loop reads.
const WriterReportFence = "dross-writer-report"

// WriterPromptLine is the whole prompt a writer spawn carries: the phase and
// task, nothing else. The writer reads its assignment through dross itself,
// so the main loop never pastes the task, its files or a plan into it.
func WriterPromptLine(phase, task string) string {
	return "Write task " + task + " of phase " + phase + ": start with `dross task show " + phase + " " + task + "`."
}
