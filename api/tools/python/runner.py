import os
import sys
import json
import ast
import traceback

def exec_code(code, globals):
    a = ast.parse(code)
    last_expression = None
    if a.body:
        if isinstance(a_last := a.body[-1], ast.Expr):
            last_expression = ast.unparse(a.body.pop())
        elif isinstance(a_last, ast.Assign):
            last_expression = ast.unparse(a_last.targets[0])
        elif isinstance(a_last, (ast.AnnAssign, ast.AugAssign)):
            last_expression = ast.unparse(a_last.target)
    exec(ast.unparse(a), globals)
    if last_expression:
        value = eval(last_expression, globals)
        if value is not None:
            print(value)

def main(raw_code):
    if raw_code is None or raw_code.strip() == "":
        print("Error: no code to execute", file=sys.stderr)
        return 1

    try:
        code = json.loads(raw_code)
    except json.decoder.JSONDecodeError:
        print("Error: JSON decode failed", file=sys.stderr)
        return 1

    try:
        exec_code(code, {})
    except Exception as exc:
        tb = traceback.format_exception_only(type(exc), exc)
        print("\n".join(tb), file=sys.stderr, end="")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(os.getenv("USER_CODE")))
