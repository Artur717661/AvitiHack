"""Local intent inference; authorization and state belong to the caller."""
from pathlib import Path
from typing import Any

from ml.router_contract import render_prompt, parse_model_response


class LocalIntentRouter:
    """Load one local base (optionally with one local LoRA adapter) and reuse it."""
    def __init__(self, model_path: str, adapter_path: str, *, device: str = 'auto'):
        import torch
        from peft import PeftModel
        from transformers import AutoModelForCausalLM, AutoTokenizer

        source = Path(model_path).expanduser().resolve()
        adapter = Path(adapter_path).expanduser().resolve()
        if not source.is_dir() or not (adapter / 'adapter_model.safetensors').is_file():
            raise ValueError('Missing local base model or LoRA adapter')
        if device == 'auto':
            device = 'cuda' if torch.cuda.is_available() else 'cpu'
        dtype = torch.bfloat16 if device == 'cuda' and torch.cuda.is_bf16_supported() else torch.float32
        self.tokenizer = AutoTokenizer.from_pretrained(source, local_files_only=True, trust_remote_code=False)
        base = AutoModelForCausalLM.from_pretrained(
            source, local_files_only=True, trust_remote_code=False, use_safetensors=True, dtype=dtype,
        )
        self.model = PeftModel.from_pretrained(base, str(adapter), is_trainable=False, local_files_only=True).to(device)
        self.model.eval()

    def predict(self, request: dict[str, Any]) -> dict[str, str]:
        """Validate backend context, project only message/state, return strict intent JSON."""
        import torch
        prompt = render_prompt(self.tokenizer, request)
        batch = self.tokenizer(prompt, add_special_tokens=False, return_tensors='pt')
        length = int(batch['input_ids'].shape[-1])
        if length > 512:
            raise ValueError(f'input requires {length} tokens; no truncation allowed')
        batch = {key: value.to(self.model.device) for key, value in batch.items()}
        with torch.inference_mode():
            generated = self.model.generate(**batch, do_sample=False, max_new_tokens=48,
                pad_token_id=self.tokenizer.pad_token_id, eos_token_id=self.tokenizer.eos_token_id)
        tokens = generated[0, length:].tolist()
        eos = self.tokenizer.eos_token_id
        if eos not in tokens:
            raise ValueError('generation did not reach EOS within the token budget')
        end = tokens.index(eos)
        if any(token != self.tokenizer.pad_token_id for token in tokens[end + 1:]):
            raise ValueError('unexpected generated content after EOS')
        return parse_model_response(self.tokenizer.decode(tokens[:end], skip_special_tokens=False))
