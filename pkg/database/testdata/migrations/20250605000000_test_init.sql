-- +goose Up
CREATE TABLE TestTable (
    ID INTEGER PRIMARY KEY,
    Name TEXT NOT NULL
);

-- +goose Down
DROP TABLE TestTable;
