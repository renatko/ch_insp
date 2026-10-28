# Project guide

## Purpose

Build a terminal UI (TUI) for visually inspecting data flows in ClickHouse through materialized views. Help an operator understand how source tables, materialized views, and destination tables are connected and how data moves between them.

## Product direction

- Make the flow of data understandable from the terminal, including the relationships between source tables, materialized views, and their targets.
- Favor clear, compact views that work well in a terminal and support interactive exploration.
- Keep ClickHouse access read-only unless a feature explicitly requires writes and the user asks for it.
- Present query or connection errors in a way that helps the operator diagnose the issue without hiding the underlying ClickHouse message.
- Do not assume a programming language, TUI framework, connection configuration format, or minimum ClickHouse version until the project establishes one.

## Working in this repository

- Check the existing files and conventions before introducing a framework or dependency.
- Keep ClickHouse discovery and querying separate from terminal rendering and interaction so each can evolve independently.
- Treat discovered schema and view definitions as user data: avoid destructive operations and avoid logging credentials or sensitive connection settings.
- When implementing behavior, describe its assumptions and verify it with the narrowest relevant checks available in the project.
- Update project documentation when a user-facing workflow or configuration format is introduced.

