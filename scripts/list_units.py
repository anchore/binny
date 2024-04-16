import subprocess
import sys

def go_list_exclude_pattern(owner, project):
    exclude_pattern = f"{owner}/{project}/test"

    result = subprocess.run(["go", "list", "./..."], stdout=subprocess.PIPE, text=True, check=True)

    filtered_lines = [line for line in result.stdout.splitlines() if exclude_pattern not in line]

    joined_output = ' '.join(filtered_lines)

    return joined_output

owner = sys.argv[1]
project = sys.argv[2]
output = go_list_exclude_pattern(owner, project)
print(output)
