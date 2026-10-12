package tasks

import (
	"strings"

	"agora/internal/javacompat"
)

// customCommandPrefix is AgoraCustomCommandHelper.CUSTOM_COMMAND_PREFIX.
const customCommandPrefix = "--run-custom-command="

// CustomCommand is AgoraCustomCommand: the command name and its "--key=value"
// arguments (nil when there is none, like Kotlin's null map).
type CustomCommand struct {
	Command   string
	Arguments map[string]string
}

// ParseCustomCommand reproduces AgoraCustomCommandHelper.storeCustomCommand and
// getStoredCustomCommandAndClear for the process arguments (main(args)):
//   - the FIRST argument starting with "--run-custom-command=" gives the command,
//     trimmed with Kotlin's trim(); a blank command is no custom command at all
//     (the web server starts);
//   - the arguments after it are joined with "\n" (nothing when the join is blank),
//     split again on "\n" (so an argument containing a newline yields several), the
//     blank pieces are dropped, and every "--key=value" piece gives an entry (the
//     last value of a key wins). Other pieces are ignored.
func ParseCustomCommand(args []string) (CustomCommand, bool) {
	for i, arg := range args {
		if !strings.HasPrefix(arg, customCommandPrefix) {
			continue
		}
		command := javacompat.KotlinTrim(arg[len(customCommandPrefix):])
		if javacompat.KotlinIsBlank(command) {
			return CustomCommand{}, false
		}
		return CustomCommand{Command: command, Arguments: parseCustomCommandArguments(args[i+1:])}, true
	}
	return CustomCommand{}, false
}

func parseCustomCommandArguments(args []string) map[string]string {
	joined := strings.Join(args, "\n")
	if javacompat.KotlinIsBlank(joined) {
		return nil
	}
	out := map[string]string{}
	for _, piece := range strings.Split(joined, "\n") {
		if javacompat.KotlinIsBlank(piece) {
			continue
		}
		if !strings.HasPrefix(piece, "--") || !strings.Contains(piece, "=") {
			continue
		}
		// substringAfter("--").substringBefore("=") and substringAfter("=")
		eq := strings.Index(piece, "=")
		out[piece[2:eq]] = piece[eq+1:]
	}
	return out
}
