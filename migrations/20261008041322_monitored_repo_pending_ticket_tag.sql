-- sqlite-migrate: checksum 82a8772009d0e45cbaf424ead6480711bd2c21b053198cfdc9c80fd6f2b66352

ALTER TABLE monitored_repos ADD COLUMN pending_ticket_tag text;
