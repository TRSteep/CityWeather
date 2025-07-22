import requests
from datetime import datetime, date, timedelta
import matplotlib.pyplot as plt
import matplotlib.dates as mdates
from geopy.geocoders import Nominatim
from geopy.exc import GeocoderUnavailable, GeocoderTimedOut
import os
from dotenv import load_dotenv

# Загрузка API ключа из .env файла
load_dotenv()
API_KEY = os.getenv('VISUAL_CROSSING_API_KEY')

if not API_KEY:
    print("⚠ Ошибка: Не найден API ключ Visual Crossing. Пожалуйста, создайте файл .env с ключом.")
    exit(1)


def get_coordinates(city):
    """
    Получает координаты города через Nominatim API.
    По умолчанию ищет в России, но поддерживает международные города.
    Возвращает (широта, долгота) или координаты Уфы (54.73780, 55.94188) при ошибке.
    """
    # Если города нет в словаре, используем API Nominatim
    try:
        geolocator = Nominatim(user_agent="weather_app")
        # Сначала пробуем искать с привязкой к России
        location = geolocator.geocode(f"{city}, Россия", exactly_one=True)
        # Если не найдено в России, ищем без указания страны
        if not location:
            location = geolocator.geocode(city, exactly_one=True)

        if location:
            return location.latitude, location.longitude
        else:
            print(f"⚠ Город '{city}' не найден. Используются координаты Уфы.")
            return 54.73780, 55.94188

    except (GeocoderUnavailable, GeocoderTimedOut, requests.exceptions.RequestException) as e:
        print(f"⚠ Ошибка геокодинга: {e}. Используются координаты Уфы.")
        return 54.73780, 55.94188


def fetch_weather_data(latitude, longitude, start_date, end_date):
    """Запрашивает данные о погоде с Visual Crossing Weather API"""
    base_url = "https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services/timeline/"

    # Формируем URL запроса
    location = f"{latitude},{longitude}"
    url = f"{base_url}{location}/{start_date}/{end_date}?unitGroup=metric&include=days&key={API_KEY}&contentType=json"

    try:
        response = requests.get(url)
        response.raise_for_status()
        data = response.json()

        # Обрабатываем данные в едином формате
        processed_data = {
            "daily": {
                "time": [],
                "temperature_2m_max": [],
                "temperature_2m_min": []
            }
        }

        for day in data.get('days', []):
            processed_data["daily"]["time"].append(day['datetime'])
            processed_data["daily"]["temperature_2m_max"].append(day['tempmax'])
            processed_data["daily"]["temperature_2m_min"].append(day['tempmin'])

        return processed_data

    except requests.exceptions.RequestException as e:
        print(f"⚠ Ошибка при запросе данных о погоде: {e}")
        return None


def plot_weather(data, city):
    """Строит график температуры с аннотациями и легендой"""
    if not data or "daily" not in data:
        print("⚠ Нет данных для построения графика!")
        return

    dates = [datetime.strptime(d, "%Y-%m-%d").date() for d in data["daily"]["time"]]
    temp_max = data["daily"]["temperature_2m_max"]
    temp_min = data["daily"]["temperature_2m_min"]

    plt.figure(figsize=(12, 6))

    # Графики с маркерами
    max_line, = plt.plot(dates, temp_max, label="Макс. температура", marker="o", color="red", linestyle="-",
                         linewidth=2)
    min_line, = plt.plot(dates, temp_min, label="Мин. температура", marker="o", color="blue", linestyle="-",
                         linewidth=2)

    # Аннотации для максимальной температуры
    for date_, temp in zip(dates, temp_max):
        plt.annotate(
           f"{temp:.1f}°C",
            (date_, temp),
            textcoords="offset points",
            xytext=(0, 10),
            ha="center",
            fontsize=9,
            color="red",
        )

    # Аннотации для минимальной температуры
    for date_, temp in zip(dates, temp_min):
        plt.annotate(
            f"{temp:.1f}°C",
            (date_, temp),
            textcoords="offset points",
            xytext=(0, -15),
            ha="center",
            fontsize=9,
            color="blue",
        )

    # Заливка между графиками
    plt.fill_between(dates, temp_min, temp_max, color="lightgray", alpha=0.3, label="Разница температур")

    # Настройка легенды
    plt.legend(
        handles=[max_line, min_line],
        loc="upper left",
        framealpha=1,
        shadow=True,
        fontsize=10,
    )

    # Настройка осей и заголовка
    plt.title(f"Температура в {city.capitalize()} за последние 7 дней", fontsize=14, pad=20)
    plt.xlabel("Дата", fontsize=12)
    plt.ylabel("Температура (°C)", fontsize=12)
    plt.grid(True, linestyle="--", alpha=0.7)

    # Форматирование дат
    plt.gca().xaxis.set_major_formatter(mdates.DateFormatter("%d.%m"))
    plt.gca().xaxis.set_major_locator(mdates.DayLocator())
    plt.gcf().autofmt_xdate()

    plt.tight_layout()
    plt.show()


def main():
    print("🌦️ Программа для анализа погоды (Visual Crossing Weather)")
    print("------------------------------------------------------")

    city = input("Введите город (Enter для Уфы): ").strip()
    if city == "":
        city = "Уфа"
    date_str = input("Введите дату в формате ГГГГ-ММ-ДД (Enter для последних 7 дней): ").strip()

    latitude, longitude = get_coordinates(city)

    if date_str:
        # Погода на конкретную дату
        try:
            #target_date = datetime.strptime(date_str, "%Y-%m-%d").date()
            data = fetch_weather_data(latitude, longitude, date_str, date_str)
            print(f"Координаты города {city}: {latitude}, {longitude}")

            if data and "daily" in data:
                temp_max = data["daily"]["temperature_2m_max"][0]
                temp_min = data["daily"]["temperature_2m_min"][0]
                print(f"\n📅 Погода в {city.capitalize()} на {date_str}:")
                print(f"🔥 Максимальная температура: {temp_max}°C")
                print(f"❄️ Минимальная температура: {temp_min}°C")
            else:
                print("Ошибка: Нет данных на эту дату.")
        except ValueError:
            print("⚠ Неверный формат даты!")
    else:
        # Погода за последние 7 дней (включая сегодня)
        end_date = date.today()
        start_date = end_date - timedelta(days=6)  # 7 дней включая сегодня

        data = fetch_weather_data(latitude, longitude, start_date.strftime("%Y-%m-%d"), end_date.strftime("%Y-%m-%d"))

        if data and "daily" in data:
            print(f"Координаты города {city}: {latitude}, {longitude}")
            plot_weather(data, city)
        else:
            print("Ошибка: Не удалось получить данные.")


if __name__ == "__main__":
    main()