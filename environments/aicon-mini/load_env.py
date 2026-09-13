import subprocess
from verifiers import vf

class _Env(vf.Environment):
    def __init__(self, cfg: vf.EnvCfg):
        # Launch the binary once
        self.proc = subprocess.Popen(
            ["../../main"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

    def search(self, query: str, budget: int = 120, k: int = 5) -> list[dict]:
        # Very light demo – echo the query
        return [{"mock": query}]

def load_environment(cfg: vf.EnvCfg) -> vf.Environment:
    return _Env(cfg)