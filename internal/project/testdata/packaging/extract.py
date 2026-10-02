"""Extract PyPA packaging's PEP 440 and PEP 508 test vectors as JSON.

Run from a checkout of pypa/packaging at the tag named in README.md:

    python3 extract.py /path/to/packaging/tests > vectors.json

It reads the test modules' literals and parametrize lists with ast,
evaluating each expression with only itertools and operator in scope, and
never imports packaging or pytest.
"""

import ast
import itertools
import json
import operator
import sys
import types


def module(path):
    with open(path) as f:
        return ast.parse(f.read())


def assigned(tree, name):
    for node in tree.body:
        if isinstance(node, ast.Assign) and any(getattr(t, "id", None) == name for t in node.targets):
            return node.value
    raise KeyError(name)


def parametrized(tree, test):
    for node in ast.walk(tree):
        if isinstance(node, ast.FunctionDef) and node.name == test:
            for decorator in node.decorator_list:
                if isinstance(decorator, ast.Call) and getattr(decorator.func, "attr", None) == "parametrize":
                    return decorator.args[1]
    raise KeyError(test)


def evaluate(node, scope):
    return eval(compile(ast.Expression(node), "<vectors>", "eval"), {"itertools": itertools, "operator": operator, **scope})


tests = sys.argv[1]
version = module(f"{tests}/test_version.py")
specifiers = module(f"{tests}/test_specifiers.py")
markers = module(f"{tests}/test_markers.py")

versions = evaluate(assigned(version, "VERSIONS"), {})
marker_scope = {name: evaluate(assigned(markers, name), {}) for name in ("VARIABLES", "OPERATORS", "VALUES")}
vectors = {
    "versions": {
        "ordered": versions,
        "invalid": evaluate(parametrized(version, "test_invalid_versions"), {}),
    },
    "specifiers": {
        "contains": [list(case) for case in evaluate(parametrized(specifiers, "test_specifiers"), {})],
    },
    "markers": {
        "valid": evaluate(parametrized(markers, "test_parses_valid"), marker_scope),
        "invalid": evaluate(parametrized(markers, "test_parses_invalid"), marker_scope),
        # The cases whose environment is the running interpreter's are
        # left out: they're the host's, not the marker's.
        "evaluates": [list(case) for case in evaluate(parametrized(markers, "test_evaluates"), {"os": types.SimpleNamespace(name="posix")}) if case[1] is not None],
    },
}
json.dump(vectors, sys.stdout, indent=1, ensure_ascii=False)
print()
