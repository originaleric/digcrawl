#!/usr/bin/env node
import { readFile } from 'node:fs/promises';
import { install, status } from './install.mjs';

const pkg = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));
const help = `DigCrawl Skill installer ${pkg.version}

Usage:
  npx digcrawl-skill install [codex|claude-code|--all] [options]
  npx digcrawl-skill status

Default: install the portable Skill, configure Codex, and install the Claude
Code Skill when detected. Other MCP hosts use the printed stdio configuration.

Options:
  --version <tag>         Go release to install (default: v${pkg.digcrawlRelease})
  --install-dir <path>    Stable program directory (default: ~/.local/bin)
  --skip-codex            Skip Codex Skill and MCP registration
  --skip-agent-setup      Install the program only
  -h, --help             Show help

Requires Node.js 20.19+. No Go, sudo, or npm lifecycle scripts required.
Restart your agent after installation. Shell configuration is never edited.`;

try {
  const [command, ...args] = process.argv.slice(2);
  if (!command || command === '--help' || command === '-h' || args.includes('--help')) {
    console.log(help);
  } else if (command === '--version') {
    console.log(pkg.version);
  } else if (command === 'status' && args.length === 0) {
    await status();
  } else if (command === 'install') {
    const options = { version: process.env.DIGCRAWL_VERSION || pkg.digcrawlRelease };
    for (let i = 0; i < args.length; i++) {
      const arg = args[i];
      if (['codex', 'claude-code', 'claude', '--all'].includes(arg)) {
        if (options.target) throw new Error('Choose one target or --all');
        options.target = arg === 'claude' ? 'claude-code' : arg;
      } else if (['--version', '--install-dir'].includes(arg)) {
        const value = args[++i];
        if (!value || value.startsWith('--')) throw new Error(`${arg} requires a value`);
        options[arg === '--version' ? 'version' : 'installDir'] = value;
      } else if (arg === '--skip-codex') options.skipCodex = true;
      else if (arg === '--skip-agent-setup') options.skipAgents = true;
      else throw new Error(`Unknown option: ${arg}`);
    }
    await install(options);
  } else throw new Error(`Unknown command: ${command}. Run with --help.`);
} catch (error) {
  console.error(`DigCrawl installation failed: ${error.message}`);
  process.exitCode = 1;
}
