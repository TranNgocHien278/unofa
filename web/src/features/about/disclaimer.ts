// Generated verbatim from docs/spec/01-product.md §Nội dung pháp lý. Edit the spec, then regenerate.
export interface Section {
  n: number
  title: string
  body: string
  bullets: string[]
}

export const DISCLAIMER: Record<'en' | 'vi', { title: string; sections: Section[] }> = {
  "en": {
    "title": "Disclaimer",
    "sections": [
      {
        "n": 1,
        "title": "About this project.",
        "body": "UNOFA (Uno Friend Arena) is a non-commercial student project built to practise system design, web development, databases, CI/CD and security. It is not a business and does not make money.",
        "bullets": []
      },
      {
        "n": 2,
        "title": "Not affiliated with UNO or Mattel.",
        "body": "UNO is a registered trademark of Mattel, Inc. UNOFA is not affiliated with, endorsed by, or sponsored by Mattel. UNOFA uses only the publicly known rules of the game, with its own card design and artwork.",
        "bullets": []
      },
      {
        "n": 3,
        "title": "U is not money.",
        "body": "UNOd (U) is a play currency that exists only inside UNOFA.",
        "bullets": [
          "U cannot be bought. There is no deposit and no payment of any kind.",
          "U cannot be sold, withdrawn, or exchanged for real money, goods, or services.",
          "U has no value outside UNOFA.",
          "Buying, selling, or trading U or UNOFA accounts for anything of real value is forbidden. Accounts that do so will be closed."
        ]
      },
      {
        "n": 4,
        "title": "Not gambling.",
        "body": "Because U cannot be bought or cashed out, betting U is part of the game, not gambling. Nothing of real value can be won or lost.",
        "bullets": []
      },
      {
        "n": 5,
        "title": "Loans are a game mechanic.",
        "body": "In-game loans, interest and seizure apply only to U. They are not real credit and create no real debt.",
        "bullets": []
      },
      {
        "n": 6,
        "title": "U can be lost.",
        "body": "U may be lost through normal play, through loan seizure, through bugs, or when a match is cancelled after a server restart. Bets are deducted when a match starts and are not refunded on disconnection. The project may reset balances, accounts or all data at any time, with or without notice. No compensation is given for lost U.",
        "bullets": []
      },
      {
        "n": 7,
        "title": "No warranty.",
        "body": "UNOFA is provided \"as is\". It may be unavailable, slow, or contain bugs. Use it at your own risk.",
        "bullets": []
      },
      {
        "n": 8,
        "title": "Data we store.",
        "body": "Username, a hashed password (never the password itself), language setting, your balance and transaction history, match history, and request logs including IP address and time. Request logs are deleted after 60 days. This data is used only to run and secure UNOFA. It is not sold or shared. To delete your account and data, contact the project owner.",
        "bullets": []
      },
      {
        "n": 9,
        "title": "Fair play.",
        "body": "Do not use scripts, bots, or exploits, and do not attack the service. Security testing is allowed only with written permission from the project owner. Accounts that break these rules may be closed and their U removed.",
        "bullets": []
      },
      {
        "n": 10,
        "title": "Contact.",
        "body": "unofa.contact@gmail.com",
        "bullets": []
      }
    ]
  },
  "vi": {
    "title": "Miễn trừ trách nhiệm",
    "sections": [
      {
        "n": 1,
        "title": "Về dự án.",
        "body": "UNOFA (Uno Friend Arena) là dự án sinh viên phi thương mại, làm ra để luyện system design, phát triển web, cơ sở dữ liệu, CI/CD và bảo mật. Đây không phải doanh nghiệp và không thu tiền.",
        "bullets": []
      },
      {
        "n": 2,
        "title": "Không liên kết với UNO hay Mattel.",
        "body": "UNO là nhãn hiệu đã đăng ký của Mattel, Inc. UNOFA không liên kết, không được Mattel bảo trợ hay tài trợ. UNOFA chỉ dùng luật chơi phổ biến công khai, với thiết kế lá bài và hình ảnh riêng.",
        "bullets": []
      },
      {
        "n": 3,
        "title": "U không phải tiền.",
        "body": "UNOd (U) là tiền chơi, chỉ tồn tại trong UNOFA.",
        "bullets": [
          "Không mua được U. Không có nạp tiền hay thanh toán dưới bất kỳ hình thức nào.",
          "Không bán, rút, hay đổi U ra tiền thật, hàng hoá hoặc dịch vụ.",
          "U không có giá trị bên ngoài UNOFA.",
          "Cấm mua, bán, trao đổi U hay tài khoản UNOFA lấy bất kỳ thứ gì có giá trị thật. Tài khoản vi phạm sẽ bị đóng."
        ]
      },
      {
        "n": 4,
        "title": "Không phải cờ bạc.",
        "body": "Vì U không mua được và không rút ra được, đặt cược bằng U là một phần của trò chơi, không phải cờ bạc. Không thể thắng hay thua thứ gì có giá trị thật.",
        "bullets": []
      },
      {
        "n": 5,
        "title": "Khoản vay là cơ chế game.",
        "body": "Khoản vay, lãi suất và siết nợ trong game chỉ áp dụng cho U. Đây không phải tín dụng thật và không tạo ra khoản nợ thật.",
        "bullets": []
      },
      {
        "n": 6,
        "title": "U có thể mất.",
        "body": "U có thể mất do chơi thua, do bị siết nợ, do lỗi phần mềm, hoặc khi ván bị huỷ sau khi server khởi động lại. Tiền cược bị trừ lúc ván bắt đầu và không được hoàn khi rớt mạng. Dự án có thể reset số dư, tài khoản hoặc toàn bộ dữ liệu bất kỳ lúc nào, có hoặc không báo trước. Không bồi thường U đã mất.",
        "bullets": []
      },
      {
        "n": 7,
        "title": "Không bảo đảm.",
        "body": "UNOFA được cung cấp \"nguyên trạng\". Dịch vụ có thể ngừng, chậm hoặc có lỗi. Bạn tự chịu rủi ro khi sử dụng.",
        "bullets": []
      },
      {
        "n": 8,
        "title": "Dữ liệu được lưu.",
        "body": "Username, password đã hash (không bao giờ lưu password gốc), cài đặt ngôn ngữ, số dư và lịch sử giao dịch, lịch sử ván chơi, và log request gồm địa chỉ IP và thời gian. Log request bị xoá sau 60 ngày. Dữ liệu chỉ dùng để vận hành và bảo vệ UNOFA, không bán và không chia sẻ. Muốn xoá tài khoản và dữ liệu, liên hệ chủ dự án.",
        "bullets": []
      },
      {
        "n": 9,
        "title": "Chơi công bằng.",
        "body": "Không dùng script, bot hay lỗi hệ thống để trục lợi, không tấn công dịch vụ. Kiểm thử bảo mật chỉ được phép khi có đồng ý bằng văn bản của chủ dự án. Tài khoản vi phạm có thể bị đóng và mất U.",
        "bullets": []
      },
      {
        "n": 10,
        "title": "Liên hệ.",
        "body": "unofa.contact@gmail.com",
        "bullets": []
      }
    ]
  }
}
