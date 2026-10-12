CREATE TABLE execution_snapshots (
 account text NOT NULL, server text NOT NULL, payload jsonb NOT NULL,
 received_at timestamptz NOT NULL, PRIMARY KEY(account,server)
);
CREATE TABLE execution_deals (
 account text NOT NULL, server text NOT NULL, ticket text NOT NULL,
 broker_time bigint NOT NULL, payload jsonb NOT NULL,
 PRIMARY KEY(account,server,ticket)
);
CREATE TABLE execution_events (
 account text NOT NULL, server text NOT NULL, id text NOT NULL,
 broker_time bigint NOT NULL, payload jsonb NOT NULL,
 PRIMARY KEY(account,server,id)
);
