import { Button } from "./Button"
import { copyWords, useCopy } from "./copy"
import { Terminal } from "./Terminal"

/** A command to run in a terminal, as text, with a button that copies it. */
export function CommandBlock({ command, label = "Command" }: { command: string; label?: string }) {
  const [state, copy] = useCopy()
  return (
    <div className="flex flex-col gap-2">
      <Terminal label={label} text={command} />
      <div className="flex items-center gap-2.5">
        <Button className="self-start" onClick={() => void copy(command)}>
          Copy
        </Button>
        <span role="status" className="text-[12.5px] text-mute">
          {copyWords[state]}
        </span>
      </div>
    </div>
  )
}
