"""python -m compat <command> [args...]"""
import importlib
import sys

COMMANDS = {
    # command: module whose main(argv) -> int runs it
    "inventory": "inventory",
    "replay": "replay",
    "check": "verdict",
    "subjects": "subjects",
    "repro": "repro",
    "report": "report",
    "weights": "capture",
}


def main(argv):
    if not argv or argv[0] not in COMMANDS:
        print("usage: oracle {%s} ..." % ",".join(sorted(COMMANDS)), file=sys.stderr)
        return 2
    mod = importlib.import_module("compat." + COMMANDS[argv[0]])
    return mod.main(argv[1:]) or 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
