-- sqlite-migrate: checksum 5bf7deea5dd0834cdb3a36c098e87315515f1bc659e07f4cf1fca0f15ec3426a

CREATE TABLE session_repositories (
  session_token TEXT NOT NULL REFERENCES sessions(token) ON DELETE CASCADE,
  repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
  PRIMARY KEY (session_token, repository_id)
) STRICT;

CREATE INDEX idx_session_repositories_repository ON session_repositories (repository_id);
