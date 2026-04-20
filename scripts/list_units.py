import subprocess
import sys

def go_list_exclude_pattern(owner, project):
    exclude_pattern = f"{owner}/{project}/test"

    # restrict to packages that contain test files so `go test -coverprofile`
    # does not drag test-less packages through the covdata toolchain
    result = subprocess.run(
        ["go", "list", "-f", "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}", "./..."],
        stdout=subprocess.PIPE,
        text=True,
        check=True,
    )

    filtered_lines = [line for line in result.stdout.splitlines() if line and exclude_pattern not in line]

    joined_output = ' '.join(filtered_lines)

    return joined_output

owner = sys.argv[1]
project = sys.argv[2]
output = go_list_exclude_pattern(owner, project)
print(output)
