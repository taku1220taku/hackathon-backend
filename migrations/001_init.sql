CREATE TABLE IF NOT EXISTS users (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  email VARCHAR(255) NOT NULL UNIQUE,
  password_hash VARCHAR(255) NOT NULL,
  display_name VARCHAR(120) NOT NULL,
  avatar_url TEXT,
  role ENUM('user', 'admin') NOT NULL DEFAULT 'user',
  rating DECIMAL(2,1) NOT NULL DEFAULT 5.0,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS categories (
  id BIGINT PRIMARY KEY,
  parent_id BIGINT NULL,
  name VARCHAR(120) NOT NULL,
  display_order INT NOT NULL DEFAULT 0,
  FOREIGN KEY (parent_id) REFERENCES categories(id)
);

INSERT INTO categories (id, parent_id, name, display_order) VALUES
  (100, NULL, 'レディース', 10),
  (101, 100, 'トップス', 11),
  (102, 100, 'ジャケット/アウター', 12),
  (103, 100, 'バッグ', 13),
  (104, 100, '靴', 14),
  (200, NULL, 'メンズ', 20),
  (201, 200, 'トップス', 21),
  (202, 200, 'ジャケット/アウター', 22),
  (203, 200, 'バッグ', 23),
  (204, 200, '靴', 24),
  (300, NULL, '家電・スマホ・カメラ', 30),
  (301, 300, 'スマートフォン/携帯電話', 31),
  (302, 300, 'PC/タブレット', 32),
  (303, 300, 'カメラ', 33),
  (304, 300, 'オーディオ機器', 34),
  (400, NULL, '本・音楽・ゲーム', 40),
  (401, 400, '本', 41),
  (402, 400, '漫画', 42),
  (403, 400, 'CD/DVD/ブルーレイ', 43),
  (404, 400, 'ゲーム', 44),
  (500, NULL, 'おもちゃ・ホビー・グッズ', 50),
  (501, 500, 'キャラクターグッズ', 51),
  (502, 500, '楽器/機材', 52),
  (503, 500, 'トレーディングカード', 53),
  (600, NULL, 'スポーツ・レジャー', 60),
  (601, 600, 'アウトドア', 61),
  (602, 600, 'スポーツ用品', 62),
  (700, NULL, 'コスメ・香水・美容', 70),
  (701, 700, 'ベースメイク', 71),
  (702, 700, '香水', 72),
  (800, NULL, 'その他', 80),
  (801, 800, 'その他', 81)
ON DUPLICATE KEY UPDATE name = VALUES(name), parent_id = VALUES(parent_id), display_order = VALUES(display_order);

CREATE TABLE IF NOT EXISTS items (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  seller_id BIGINT NOT NULL,
  title VARCHAR(160) NOT NULL,
  description TEXT NOT NULL,
  price INT NOT NULL,
  shipping_fee INT NOT NULL DEFAULT 0,
  category_id BIGINT NOT NULL DEFAULT 801,
  category VARCHAR(80) NOT NULL,
  status ENUM('draft', 'published', 'sold') NOT NULL DEFAULT 'draft',
  condition_score INT NOT NULL DEFAULT 0,
  context TEXT,
  seller_hidden BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (seller_id) REFERENCES users(id),
  FOREIGN KEY (category_id) REFERENCES categories(id)
);

CREATE TABLE IF NOT EXISTS item_images (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  item_id BIGINT NOT NULL,
  image_url MEDIUMTEXT NOT NULL,
  display_order INT NOT NULL DEFAULT 0,
  FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS item_likes (
  user_id BIGINT NOT NULL,
  item_id BIGINT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id, item_id),
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS item_views (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  item_id BIGINT NOT NULL,
  viewer_id BIGINT NULL,
  viewer_hash CHAR(64) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_item_views_item_created (item_id, created_at),
  INDEX idx_item_views_viewer (item_id, viewer_hash, created_at),
  FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE,
  FOREIGN KEY (viewer_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS transactions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  item_id BIGINT NOT NULL,
  buyer_id BIGINT NOT NULL,
  seller_id BIGINT NOT NULL,
  status ENUM('pending', 'active', 'done') NOT NULL DEFAULT 'pending',
  buyer_hidden BOOLEAN NOT NULL DEFAULT FALSE,
  seller_hidden BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  completed_at TIMESTAMP NULL,
  FOREIGN KEY (item_id) REFERENCES items(id),
  FOREIGN KEY (buyer_id) REFERENCES users(id),
  FOREIGN KEY (seller_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS messages (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  transaction_id BIGINT NOT NULL,
  sender_id BIGINT NOT NULL,
  body TEXT NOT NULL,
  sent_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (transaction_id) REFERENCES transactions(id) ON DELETE CASCADE,
  FOREIGN KEY (sender_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS reviews (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  transaction_id BIGINT NOT NULL,
  reviewer_id BIGINT NOT NULL,
  reviewee_id BIGINT NOT NULL,
  rating INT NOT NULL,
  comment TEXT,
  FOREIGN KEY (transaction_id) REFERENCES transactions(id),
  FOREIGN KEY (reviewer_id) REFERENCES users(id),
  FOREIGN KEY (reviewee_id) REFERENCES users(id)
);
